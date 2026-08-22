// Package onkimport converts OpenAPI 3.x documents into .onk schema source.
// It is intentionally opinionated: deterministic output, local $refs only,
// and warnings (never errors) for constructs without a faithful .onk mapping.
//
// Modeling rules:
//   - every operation becomes <Op>Request / <Op>Response messages plus an RPC
//     referencing them, so path/query/body parameters live as request fields;
//   - @query binding is only legal on non-body verbs, so operations with a
//     requestBody fold query parameters into plain fields with a warning;
//   - component schemas are memoized under their canonical name before
//     recursion, making self-referential specs terminate (recursive edges
//     degrade to json with a warning).
package onkimport

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxDepth = 32

// Options tunes conversion.
type Options struct {
	// Package is the .onk package declaration.
	Package string
	// Service is the generated service name (default: Api + "Service").
	Service string
}

// Result carries the converted schema plus non-fatal diagnostics.
type Result struct {
	Source   []byte
	Package  string
	Warnings []string
}

type importer struct {
	root map[string]any
	opts Options

	warnings  []string
	messages  map[string][]string // name -> field lines; nil while converting
	orderMsg  []string
	errorMsgs map[string]bool
	enums     map[string][]string // name -> member lines
	orderEnum []string

	usedNames map[string]bool
	refDone   map[string]fieldType
	refActive map[string]bool
}

// Import converts an OpenAPI 3.x document (YAML or JSON) into .onk source.
func Import(data []byte, opts Options) (*Result, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	if root == nil {
		return nil, errors.New("empty OpenAPI document")
	}
	if v := text(root, "openapi"); !strings.HasPrefix(v, "3") {
		return nil, fmt.Errorf("unsupported OpenAPI version %q; only 3.x is supported", v)
	}
	pkg := opts.Package
	if pkg == "" {
		pkg = slug(text(asMap(root["info"]), "title"), "api")
	}
	service := opts.Service
	if service == "" {
		service = Pascal(pkg) + "Service"
	}
	im := &importer{
		root:      root,
		opts:      Options{Package: pkg, Service: service},
		messages:  map[string][]string{},
		errorMsgs: map[string]bool{},
		enums:     map[string][]string{},
		usedNames: map[string]bool{},
		refDone:   map[string]fieldType{},
		refActive: map[string]bool{},
	}
	rpcs, err := im.convertPaths()
	if err != nil {
		return nil, err
	}
	return &Result{
		Source:   im.render(rpcs),
		Package:  pkg,
		Warnings: im.warnings,
	}, nil
}

// --- paths -----------------------------------------------------------------

var httpMethods = []string{"get", "put", "post", "delete", "patch"}

func (im *importer) convertPaths() ([]string, error) {
	basePath := "/"
	if server := firstMap(asSlice(im.root["servers"])); server != nil {
		if raw := text(server, "url"); raw != "" {
			basePath = urlPath(raw)
		}
	}
	paths := asMap(im.root["paths"])
	keys := make([]string, 0, len(paths))
	for key := range paths {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var rpcs []string
	for _, pathKey := range keys {
		item := asMap(paths[pathKey])
		if item == nil {
			continue
		}
		sharedParams := asSlice(item["parameters"])
		for _, method := range httpMethods {
			op := asMap(item[method])
			if op == nil {
				continue
			}
			params := append(append([]any{}, sharedParams...), asSlice(op["parameters"])...)
			rpcs = append(rpcs, im.convertOperation(op, method, pathKey, params))
		}
	}
	if len(rpcs) == 0 {
		return nil, errors.New("document declares no operations")
	}
	return append([]string{"base_path: \"" + basePath + "\""}, rpcs...), nil
}

func (im *importer) convertOperation(op map[string]any, method, pathKey string, params []any) string {
	opName := im.operationName(op, method, pathKey)

	reqName := im.registerMessage(opName + "Request")

	// @query binding is only legal on non-body verbs, so detect a request
	// body before emitting any parameters: query params on body-bearing
	// operations must fold into plain fields.
	requestBody := asMap(op["requestBody"])
	hasBody := false
	if requestBody != nil {
		if _, ok := im.jsonSchema(asMap(requestBody["content"])); ok {
			hasBody = true
		}
	}
	var queryLines, otherLines []string
	for _, rawParam := range params {
		param := asMap(rawParam)
		if param == nil {
			continue
		}
		name := text(param, "name")
		in := text(param, "in")
		if name == "" || in == "header" || in == "cookie" {
			if in == "header" {
				im.warnf("%s: header parameter %q skipped; declare header contracts explicitly", opName, name)
			}
			continue
		}
		field := safeIdent(name)
		if field != name && in == "path" {
			im.warnf("%s: path parameter %q renamed to %q", opName, name, field)
		}
		ft, ok := im.schemaTypeExpr(param["schema"], reqName+Pascal(field), 1)
		if !ok || ft.expr == "" {
			ft = fieldType{expr: "string"}
		}
		line := composeFieldLine(field, ft, !truthy(param["required"]), opName, name)
		switch in {
		case "path":
			if !truthy(param["required"]) && !strings.HasSuffix(line, "?") {
				line += "?"
			}
			otherLines = append(otherLines, line)
		case "query":
			switch {
			case hasBody:
				im.warnf("%s: query parameter %q folded without binding (@query is reserved for non-body verbs)", opName, name)
				queryLines = append(queryLines, line)
			case name == field:
				queryLines = append(queryLines, line+" @query")
			default:
				queryLines = append(queryLines, line+" @query(\""+name+"\")")
			}
		}
	}
	if requestBody != nil {
		if schema, ok := im.jsonSchema(asMap(requestBody["content"])); ok {
			ft, _ := im.schemaTypeExpr(schema, opName+"Body", 1)
			// @body is declared at RPC level; the field itself stays plain.
			line := composeFieldLine("body", ft, !truthy(requestBody["required"]), opName, "body")
			otherLines = append(otherLines, line)
		}
	}
	im.messages[reqName] = append(otherLines, queryLines...)

	respName, union := im.responsePieces(opName, asMap(op["responses"]))
	var rpc strings.Builder
	rpc.WriteString("  " + opName + "(" + reqName + ") -> " + respName)
	for _, errName := range union {
		rpc.WriteString(" | ")
		rpc.WriteString(errName)
	}
	if hasBody {
		rpc.WriteString(" @body(\"body\")")
	}
	rpc.WriteString(" @" + method + "(\"" + pathKey + "\")")
	return rpc.String()
}

func (im *importer) operationName(op map[string]any, method, pathKey string) string {
	if id := text(op, "operationId"); id != "" {
		return im.uniqueName(capitalize(pascalIdent(id)))
	}
	derived := strings.ReplaceAll(strings.Trim(pathKey, "/"), "/", "_")
	return im.uniqueName(Pascal(method) + Pascal(slug(derived, "")))
}

// capitalize raises the first rune so camelCase ids become PascalCase
// without disturbing interior casing (showPetById -> ShowPetById).
func capitalize(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] -= 32
	}
	return string(runes)
}

// responsePieces resolves the success payload and declared error union.
func (im *importer) responsePieces(opName string, responses map[string]any) (string, []string) {
	respName := im.uniqueName(opName + "Response")
	var union []string

	successCode := pickSuccess(responses)
	schema, hasSchema := map[string]any{}, false
	if successCode != "" {
		schema, hasSchema = im.jsonSchema(asMap(asMap(responses[successCode])["content"]))
	}
	switch {
	case !hasSchema:
		if successCode == "" {
			im.warnf("%s: no 2xx response declared; empty response used", opName)
		}
		im.registerMessage(respName)
	default:
		ft, ok := im.schemaTypeExpr(schema, respName, 1)
		if !ok {
			ft = fieldType{expr: "json"}
		}
		switch {
		case im.isDecl(ft.expr):
			respName = ft.expr
		case strings.HasSuffix(ft.expr, "[]") || strings.HasPrefix(ft.expr, "map[") ||
			isScalarExpr(ft.expr):
			im.registerMessage(respName)
			im.messages[respName] = []string{"data: " + ft.expr}
		default:
			// Inline object schema already registered itself under respName.
		}
	}

	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		status, err := strconv.Atoi(code)
		if err != nil || status < 400 || status > 599 {
			continue
		}
		errSchema, ok := im.jsonSchema(asMap(asMap(responses[code])["content"]))
		if !ok {
			continue
		}
		errName := opName + "Error" + strconv.Itoa(status)
		if ft, ok2 := im.schemaTypeExpr(errSchema, errName, 1); ok2 && im.isDecl(ft.expr) {
			errName = ft.expr
		} else {
			im.registerMessage(errName)
			im.messages[errName] = []string{"message: string"}
		}
		if !im.errorMsgs[errName] {
			im.errorMsgs[errName] = true
			im.decorateStatus(errName, status)
			union = append(union, errName)
		}
	}
	return respName, union
}

func (im *importer) decorateStatus(name string, status int) {
	im.messages[name] = append([]string{"@status(" + strconv.Itoa(status) + ")"}, im.messages[name]...)
}

func pickSuccess(responses map[string]any) string {
	for _, code := range []string{"200", "201", "202", "204"} {
		if _, ok := responses[code]; ok {
			return code
		}
	}
	for _, code := range []string{"2XX", "default"} {
		if _, ok := responses[code]; ok {
			return code
		}
	}
	return ""
}

func (im *importer) jsonSchema(content map[string]any) (map[string]any, bool) {
	media := asMap(content["application/json"])
	if media == nil {
		return nil, false
	}
	schema := asMap(media["schema"])
	return schema, schema != nil
}

// --- schema conversion -----------------------------------------------------

type fieldType struct {
	expr string
	// suffix carries validator/encoding decorators that trail the type and
	// any optionality marker ("@email", " @encode(date)").
	suffix string
}

func (im *importer) schemaTypeExpr(raw any, suggested string, depth int) (fieldType, bool) {
	if depth > maxDepth {
		im.warnf("schema %q exceeds reference depth; mapped to json", suggested)
		return fieldType{expr: "json"}, true
	}
	schema := asMap(raw)
	if schema == nil {
		return fieldType{}, false
	}
	if ref := text(schema, "$ref"); ref != "" {
		return im.resolveRef(ref, suggested, depth)
	}
	if allOf := asSlice(schema["allOf"]); len(allOf) > 0 {
		merged := map[string]any{
			"type": "object",
			"properties": func() map[string]any {
				props := map[string]any{}
				for _, part := range allOf {
					sub := asMap(part)
					if sub == nil {
						continue
					}
					if ref := text(sub, "$ref"); ref != "" {
						sub = im.lookupRef(ref)
						if sub == nil {
							continue
						}
					}
					for k, v := range asMap(sub["properties"]) {
						props[k] = v
					}
				}
				return props
			}(),
		}
		if required := im.allOfRequired(allOf); len(required) > 0 {
			merged["required"] = required
		}
		return im.schemaTypeExpr(merged, suggested, depth+1)
	}
	switch text(schema, "type") {
	case "array":
		item, ok := im.schemaTypeExpr(schema["items"], singular(suggested), depth+1)
		if !ok {
			return fieldType{}, false
		}
		return fieldType{expr: item.expr + "[]"}, true
	case "object":
		props := asMap(schema["properties"])
		additional, hasAdditional := schema["additionalProperties"]
		switch {
		case hasAdditional && len(props) == 0:
			value, ok := im.schemaTypeExpr(additional, singular(suggested), depth+1)
			if !ok {
				return fieldType{}, false
			}
			return fieldType{expr: "map[string, " + value.expr + "]"}, true
		case len(props) == 0:
			return fieldType{expr: "json"}, true
		}
		name := im.registerMessage(suggested)
		var required map[string]bool
		if reqs := asSlice(schema["required"]); len(reqs) > 0 {
			required = map[string]bool{}
			for _, r := range reqs {
				required[textOf(r)] = true
			}
		} else {
			required = map[string]bool{}
		}
		im.messages[name] = im.convertProperties(name, props, required, depth+1)
		return fieldType{expr: name}, true
	case "integer":
		if text(schema, "format") == "int64" {
			return fieldType{expr: "int64"}, true
		}
		return fieldType{expr: "int32"}, true
	case "number":
		return fieldType{expr: "float64"}, true
	case "boolean":
		return fieldType{expr: "bool"}, true
	case "string":
		return im.stringFieldType(schema, suggested), true
	default:
		im.warnf("schema %q uses unsupported composition (%s); mapped to json", suggested, strings.Join(mapKeys(schema), ","))
		return fieldType{expr: "json"}, true
	}
}

func (im *importer) stringFieldType(schema map[string]any, suggested string) fieldType {
	switch text(schema, "format") {
	case "date-time":
		return fieldType{expr: "timestamp"}
	case "date":
		return fieldType{expr: "timestamp", suffix: " @encode(date)"}
	}
	if values := asSlice(schema["enum"]); len(values) > 0 {
		// Enums live in their own registry: registering through the message
		// table would emit a stray empty message sharing the name.
		base := suggested
		if !strings.HasSuffix(base, "Values") {
			base += "Values"
		}
		name := im.uniqueName(base)
		var lines []string
		for _, v := range values {
			raw := textOf(v)
			member := upperSnake(raw)
			if member == "" {
				continue
			}
			if member != raw {
				lines = append(lines, member+" @json("+strconv.Quote(raw)+")")
			} else {
				lines = append(lines, member)
			}
		}
		if len(lines) == 0 {
			return fieldType{expr: "string"}
		}
		im.enums[name] = lines
		im.orderEnum = append(im.orderEnum, name)
		return fieldType{expr: name}
	}
	expr := scalarString
	var suffix string
	switch text(schema, "format") {
	case "email":
		suffix = " @email"
	case "uuid":
		suffix = " @uuid"
	case "uri":
		suffix = " @uri"
	}
	return fieldType{expr: expr, suffix: suffix}
}

func (im *importer) convertProperties(msgName string, props map[string]any, required map[string]bool, depth int) []string {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, rawName := range names {
		field := safeIdent(rawName)
		if field != rawName {
			im.warnf("%s.%q renamed to %q", msgName, rawName, field)
		}
		ft, ok := im.schemaTypeExpr(props[rawName], msgName+Pascal(field), depth)
		if !ok || ft.expr == "" {
			im.warnf("%s.%q has no convertible schema; mapped to json", msgName, rawName)
			ft = fieldType{expr: "json"}
		}
		line := composeFieldLine(field, ft, !required[rawName], msgName, rawName)
		lines = append(lines, line)
	}
	return lines
}

// --- refs ------------------------------------------------------------------

func (im *importer) lookupRef(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		im.warnf("external $ref %q not supported", ref)
		return nil
	}
	cur := any(im.root)
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(segment, "~1", "/")
		segment = strings.ReplaceAll(segment, "~0", "~")
		asM, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = asM[segment]
	}
	asM, _ := cur.(map[string]any)
	return asM
}

func (im *importer) resolveRef(ref, suggested string, depth int) (fieldType, bool) {
	if cached, ok := im.refDone[ref]; ok {
		return cached, cached.expr != ""
	}
	if im.refActive[ref] {
		im.warnf("recursive $ref %q; edge mapped to json", ref)
		return fieldType{expr: "json"}, true
	}
	target := im.lookupRef(ref)
	if target == nil {
		return fieldType{expr: "json"}, true
	}
	// Component schemas own their canonical declaration name regardless of
	// call site, so Pet is emitted once as Pet - never per-context.
	if strings.HasPrefix(ref, "#/components/schemas/") {
		suggested = refCanonicalName(ref)
	}
	im.refActive[ref] = true
	defer delete(im.refActive, ref)

	result, ok := im.schemaTypeExpr(target, suggested, depth+1)
	if !ok {
		result = fieldType{expr: "json"}
	}
	im.refDone[ref] = result
	return result, true
}

// --- registry --------------------------------------------------------------

// registerMessage reserves a declaration slot and returns its (possibly
// disambiguated) name.
func (im *importer) registerMessage(base string) string {
	name := pascalIdent(base)
	if name == "" {
		name = "Object"
	}
	candidate := name
	for i := 2; im.usedNames[candidate]; i++ {
		candidate = name + "_" + strconv.Itoa(i)
	}
	im.usedNames[candidate] = true
	im.messages[candidate] = nil
	im.orderMsg = append(im.orderMsg, candidate)
	return candidate
}

func (im *importer) isDecl(name string) bool {
	if _, ok := im.messages[name]; ok {
		return true
	}
	_, ok := im.enums[name]
	return ok
}

// --- render ----------------------------------------------------------------

func (im *importer) render(serviceLines []string) []byte {
	var out strings.Builder
	out.WriteString("// Code generated by onek import; re-run import instead of editing.\n")
	out.WriteString("package " + im.opts.Package + "\n\n")

	msgNames := sortUnique(im.orderMsg)
	enumNames := sortUnique(im.orderEnum)

	for _, name := range msgNames {
		if im.errorMsgs[name] {
			continue
		}
		writeMessage(&out, name, im.messages[name], false)
	}
	for _, name := range enumNames {
		out.WriteString("enum " + name + " {\n")
		for i, line := range im.enums[name] {
			if i > 0 {
				out.WriteString("\n")
			}
			out.WriteString("  " + line + "\n")
		}
		out.WriteString("}\n\n")
	}
	for _, name := range msgNames {
		if im.errorMsgs[name] {
			writeMessage(&out, name, im.messages[name], true)
		}
	}

	out.WriteString("service " + im.opts.Service + " {\n")
	prevWasBasePath := false
	rpcStarted := false
	for _, line := range serviceLines {
		if !strings.HasPrefix(line, "  ") {
			line = "  " + line
		}
		if strings.HasPrefix(strings.TrimSpace(line), "base_path:") {
			prevWasBasePath = true
		} else if rpcStarted || prevWasBasePath {
			out.WriteString("\n") // canonical blanks: after base_path and between RPCs
			prevWasBasePath = false
		}
		rpcStarted = true
		out.WriteString(line + "\n")
	}
	out.WriteString("}\n")
	return []byte(out.String())
}

func writeMessage(out *strings.Builder, name string, fields []string, isError bool) {
	statusPrefix := ""
	if isError && len(fields) > 0 && strings.HasPrefix(fields[0], "@status(") {
		statusPrefix = fields[0] + " "
		fields = fields[1:]
	}
	out.WriteString("message " + name + " " + statusPrefix + "{\n")
	for i, line := range fields {
		if i > 0 {
			out.WriteString("\n") // canonical layout blanks between members
		}
		out.WriteString("  " + line + "\n")
	}
	out.WriteString("}\n\n")
}
