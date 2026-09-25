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
	"math"
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

	warnings    []string
	messages    map[string][]string // name -> field lines; nil while converting
	orderMsg    []string
	errorMsgs   map[string]bool
	errorStatus map[string]int
	enums       map[string][]string // name -> member lines
	orderEnum   []string

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
	root, _ = normalizeKeys(root).(map[string]any)
	if version, ok := root["openapi"].(float64); ok {
		root["openapi"] = strconv.FormatFloat(version, 'f', -1, 64)
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
		root:        root,
		opts:        Options{Package: pkg, Service: service},
		messages:    map[string][]string{},
		errorMsgs:   map[string]bool{},
		errorStatus: map[string]int{},
		enums:       map[string][]string{},
		usedNames:   map[string]bool{},
		refDone:     map[string]fieldType{},
		refActive:   map[string]bool{},
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
	requestBody := im.deref(asMap(op["requestBody"]))
	if requestBody != nil && method != "post" && method != "put" && method != "patch" {
		im.warnf("%s: request body on %s is not supported by onekit and was dropped", opName, strings.ToUpper(method))
		requestBody = nil
	}
	hasBody := false
	if requestBody != nil {
		if _, ok := im.jsonSchema(asMap(requestBody["content"])); ok {
			hasBody = true
		}
	}
	var queryLines, otherLines []string
	headers := im.securityHeaders(op, opName)
	route := pathKey
	for _, rawParam := range params {
		param := im.deref(asMap(rawParam))
		if param == nil {
			continue
		}
		name := text(param, "name")
		in := text(param, "in")
		if in == "header" {
			headers = appendHeader(headers, name, headerDecorators(param))
			continue
		}
		if name == "" || in == "cookie" {
			if in == "cookie" {
				im.warnf("%s: cookie parameter %q is not supported and was dropped", opName, name)
			}
			continue
		}
		field := safeIdent(name)
		if field != name && in == paramInPath {
			im.warnf("%s: path parameter %q renamed to %q", opName, name, field)
		}
		ft := im.parameterFieldType(param["schema"], reqName+Pascal(field), opName, name)
		optional := !truthy(param["required"]) && in != paramInPath
		line := composeFieldLine(field, ft, optional, opName, name)
		switch in {
		case paramInPath:
			if field != name {
				route = strings.ReplaceAll(route, "{"+name+"}", "{"+field+"}")
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
	rpc.WriteString(" @" + method + "(\"" + route + "\")")
	if len(headers) > 0 {
		rpc.WriteString(" {\n    headers: {\n")
		for _, header := range headers {
			rpc.WriteString("      " + header.line + "\n")
		}
		rpc.WriteString("    }\n  }")
	}
	return rpc.String()
}

type importedHeader struct {
	name string
	line string
}

var implicitHeaders = map[string]bool{"accept": true, "content-type": true, "content-length": true}

func appendHeader(headers []importedHeader, name, decorators string) []importedHeader {
	if name == "" || implicitHeaders[strings.ToLower(name)] {
		return headers
	}
	for _, existing := range headers {
		if strings.EqualFold(existing.name, name) {
			return headers
		}
	}
	return append(headers, importedHeader{name: name, line: strconv.Quote(name) + ": string" + decorators})
}

func headerDecorators(param map[string]any) string {
	var out string
	if truthy(param["required"]) {
		out += " @required"
	}
	switch format := text(asMap(param["schema"]), "format"); format {
	case "uuid", "email", "uri":
		out += " @format(\"" + format + "\")"
	}
	if truthy(param["deprecated"]) {
		out += " @deprecated"
	}
	return out
}

func (im *importer) securityHeaders(op map[string]any, opName string) []importedHeader {
	requirements, ok := op["security"]
	if !ok {
		requirements = im.root["security"]
	}
	options := asSlice(requirements)
	if len(options) == 0 {
		return nil
	}
	required := true
	var alternatives []map[string]any
	for _, option := range options {
		if entry := asMap(option); len(entry) > 0 {
			alternatives = append(alternatives, entry)
		} else {
			required = false
		}
	}
	if len(alternatives) == 0 {
		return nil
	}
	if len(alternatives) > 1 {
		im.warnf("%s: alternative security requirements are not supported; kept the first one", opName)
	}
	if !required {
		im.warnf("%s: optional security was imported as a required header", opName)
	}
	chosen := alternatives[0]
	names := make([]string, 0, len(chosen))
	for name := range chosen {
		names = append(names, name)
	}
	sort.Strings(names)
	schemes := asMap(asMap(im.root["components"])["securitySchemes"])
	var headers []importedHeader
	for _, name := range names {
		scheme := im.deref(asMap(schemes[name]))
		switch kind := text(scheme, "type"); {
		case kind == "apiKey" && text(scheme, "in") == "header":
			headers = appendHeader(headers, text(scheme, "name"), " @required @auth(\"api_key\") @auth_scheme_name(\""+name+"\")")
		case kind == "http" && strings.EqualFold(text(scheme, "scheme"), "bearer"):
			headers = appendHeader(headers, "Authorization", " @required @auth(\"bearer\") @auth_scheme_name(\""+name+"\")")
		case kind == "http" && strings.EqualFold(text(scheme, "scheme"), "basic"):
			headers = appendHeader(headers, "Authorization", " @required @auth(\"basic\") @auth_scheme_name(\""+name+"\")")
		default:
			im.warnf("%s: security scheme %q (%s) cannot be expressed as a header and was dropped", opName, name, kind)
		}
	}
	return headers
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
		schema, hasSchema = im.jsonSchema(asMap(im.deref(asMap(responses[successCode]))["content"]))
	}
	switch {
	case !hasSchema:
		if successCode == "" {
			im.warnf("%s: no 2xx response declared; empty response used", opName)
		}
		im.declareReserved(respName)
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
			im.declareReserved(respName)
			im.messages[respName] = []string{"data: " + ft.expr + " @unwrap"}
		default:
			// Inline object schema already registered itself under respName.
		}
	}

	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	seenStatus := map[int]bool{}
	for _, code := range codes {
		status, ok := errorStatus(code)
		if !ok || seenStatus[status] {
			continue
		}
		if code != strconv.Itoa(status) {
			im.warnf("%s: response %q imported as status %d", opName, code, status)
		}
		errSchema, ok := im.jsonSchema(asMap(im.deref(asMap(responses[code]))["content"]))
		if !ok {
			continue
		}
		errName := opName + "Error" + strconv.Itoa(status)
		if ft, ok2 := im.schemaTypeExpr(errSchema, errName, 1); ok2 && im.isDecl(ft.expr) {
			errName = ft.expr
		} else {
			errName = im.registerMessage(errName)
			im.messages[errName] = []string{"message: string"}
		}
		errName = im.errorWithStatus(errName, status)
		seenStatus[status] = true
		union = append(union, errName)
	}
	return respName, union
}

func errorStatus(code string) (int, bool) {
	switch strings.ToUpper(code) {
	case "4XX":
		return 400, true
	case "5XX", "DEFAULT":
		return 500, true
	}
	status, err := strconv.Atoi(code)
	return status, err == nil && status >= 400 && status <= 599
}

func (im *importer) errorWithStatus(name string, status int) string {
	if existing, ok := im.errorStatus[name]; ok && existing != status {
		alias := im.registerMessage(name + strconv.Itoa(status))
		im.messages[alias] = append([]string(nil), im.messages[name][1:]...)
		name = alias
	}
	if _, ok := im.errorStatus[name]; !ok {
		im.errorStatus[name] = status
		im.errorMsgs[name] = true
		im.decorateStatus(name, status)
	}
	return name
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
	for _, code := range []string{"2XX"} {
		if _, ok := responses[code]; ok {
			return code
		}
	}
	return ""
}

func (im *importer) parameterFieldType(raw any, suggested, opName, name string) fieldType {
	schema := asMap(raw)
	if ref := text(schema, "$ref"); ref != "" {
		if resolved := im.lookupRef(ref); resolved != nil {
			schema = resolved
		}
	}
	if values := asSlice(schema["enum"]); len(values) > 0 && text(schema, "type") == scalarString {
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, strconv.Quote(textOf(value)))
		}
		return fieldType{expr: scalarString, suffix: " @in(" + strings.Join(quoted, ", ") + ")"}
	}
	ft, ok := im.schemaTypeExpr(schema, suggested, 1)
	if !ok || ft.expr == "" {
		return fieldType{expr: scalarString}
	}
	if isScalarExpr(ft.expr) && ft.expr != "timestamp" && ft.expr != "json" {
		return ft
	}
	im.warnf("%s: parameter %q has type %s, which cannot bind to a URL; imported as string", opName, name, ft.expr)
	return fieldType{expr: scalarString}
}

func (im *importer) jsonSchema(content map[string]any) (map[string]any, bool) {
	var fallback map[string]any
	keys := mapKeys(content)
	sort.Strings(keys)
	for _, key := range keys {
		mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(key, ";", 2)[0]))
		media := asMap(content[key])
		schema := asMap(media["schema"])
		switch {
		case schema == nil:
		case mediaType == "application/json":
			return schema, true
		case strings.HasSuffix(mediaType, "+json") || mediaType == "*/*" || mediaType == "application/*":
			if fallback == nil {
				fallback = schema
			}
		}
	}
	if fallback != nil {
		return fallback, true
	}
	if len(keys) > 0 {
		im.warnf("content types %s are not JSON and were skipped", strings.Join(keys, ", "))
	}
	return nil, false
}

// --- schema conversion -----------------------------------------------------

type fieldType struct {
	expr     string
	nullable bool
	// suffix carries validator/encoding decorators that trail the type and
	// any optionality marker ("@email", " @encode(date)").
	suffix string
}

func schemaTypeName(schema map[string]any) (string, bool) {
	typ := ""
	nullable := schema["nullable"] == true
	switch value := schema["type"].(type) {
	case string:
		typ = value
	case []any:
		for _, item := range value {
			switch name := textOf(item); name {
			case "null":
				nullable = true
			case "":
			default:
				if typ == "" {
					typ = name
				}
			}
		}
	}
	if typ == "" {
		switch {
		case schema["properties"] != nil || schema["additionalProperties"] != nil || schema["required"] != nil:
			typ = schemaObject
		case schema["items"] != nil:
			typ = "array"
		}
	}
	return typ, nullable
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
		if len(allOf) == 1 && len(asMap(schema["properties"])) == 0 {
			if ref := text(asMap(allOf[0]), "$ref"); ref != "" {
				return im.resolveRef(ref, suggested, depth)
			}
		}
		props := map[string]any{}
		im.collectAllOfProperties(allOf, props, 0)
		for k, v := range asMap(schema["properties"]) {
			props[k] = v
		}
		merged := map[string]any{
			"type":       schemaObject,
			"properties": props,
		}
		if required := append(im.allOfRequired(allOf), asSlice(schema["required"])...); len(required) > 0 {
			merged["required"] = required
		}
		return im.schemaTypeExpr(merged, suggested, depth+1)
	}
	if typ, nullable := schemaTypeName(schema); typ != text(schema, "type") || nullable {
		normalized := make(map[string]any, len(schema))
		for key, value := range schema {
			normalized[key] = value
		}
		normalized["type"] = typ
		delete(normalized, "nullable")
		ft, ok := im.schemaTypeExpr(normalized, suggested, depth)
		ft.nullable = ft.nullable || nullable
		return ft, ok
	}
	switch text(schema, "type") {
	case "array":
		item, ok := im.schemaTypeExpr(schema["items"], singular(suggested), depth+1)
		if !ok {
			return fieldType{}, false
		}
		if item.expr == scalarInt64 {
			im.warnf("schema %q has int64 array items; onekit sends repeated int64 values as JSON strings", suggested)
		}
		return fieldType{expr: item.expr + "[]"}, true
	case schemaObject:
		props := asMap(schema["properties"])
		additional, hasAdditional := schema["additionalProperties"]
		switch {
		case hasAdditional && len(props) == 0:
			value, ok := im.schemaTypeExpr(additional, singular(suggested), depth+1)
			if !ok {
				return fieldType{}, false
			}
			if value.expr == scalarInt64 {
				im.warnf("schema %q has int64 map values; onekit sends them as JSON strings", suggested)
			}
			return fieldType{expr: "map[string, " + value.expr + "]"}, true
		case len(props) == 0:
			return fieldType{expr: "json"}, true
		}
		name := im.registerMessage(suggested)
		im.fillObject(name, schema, depth)
		return fieldType{expr: name}, true
	case "integer":
		if text(schema, "format") == "int64" {
			return fieldType{expr: scalarInt64, suffix: " @encode(number)"}, true
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
		seenMembers := map[string]bool{}
		for _, v := range values {
			raw := textOf(v)
			member := upperSnake(raw)
			if member == "" {
				continue
			}
			if member[0] >= '0' && member[0] <= '9' {
				member = "V_" + member
			}
			for base, i := member, 2; seenMembers[member]; i++ {
				member = base + "_" + strconv.Itoa(i)
			}
			seenMembers[member] = true
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
		ft.suffix += im.constraintDecorators(im.derefSchema(asMap(props[rawName])), ft.expr)
		line := composeFieldLine(field, ft, !required[rawName], msgName, rawName)
		if doc := text(asMap(props[rawName]), "description"); doc != "" {
			line = docLines(doc) + line
		}
		lines = append(lines, line)
	}
	return lines
}

func (im *importer) collectAllOfProperties(parts []any, props map[string]any, depth int) {
	if depth > maxDepth {
		return
	}
	for _, part := range parts {
		sub := asMap(part)
		if ref := text(sub, "$ref"); ref != "" {
			sub = im.lookupRef(ref)
		}
		if sub == nil {
			continue
		}
		im.collectAllOfProperties(asSlice(sub["allOf"]), props, depth+1)
		for k, v := range asMap(sub["properties"]) {
			props[k] = v
		}
	}
}

func (im *importer) derefSchema(schema map[string]any) map[string]any {
	if ref := text(schema, "$ref"); ref != "" {
		if resolved := im.lookupRef(ref); resolved != nil {
			return resolved
		}
	}
	return schema
}

func docLines(doc string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(doc), "\n") {
		b.WriteString("/// " + strings.TrimSpace(line) + "\n  ")
	}
	return b.String()
}

func (im *importer) constraintDecorators(schema map[string]any, expr string) string {
	number := func(key string) (string, bool) {
		var value float64
		switch typed := schema[key].(type) {
		case float64:
			value = typed
		case int:
			value = float64(typed)
		case int64:
			value = float64(typed)
		case uint64:
			value = float64(typed)
		default:
			return "", false
		}
		if strings.HasPrefix(expr, "int") || strings.HasPrefix(expr, "uint") {
			if value != math.Trunc(value) {
				return "", false
			}
		}
		return strconv.FormatFloat(value, 'f', -1, 64), true
	}
	var out strings.Builder
	switch {
	case expr == scalarString:
		if maxLength, ok := number("maxLength"); ok {
			minLength, hasMin := number("minLength")
			if !hasMin {
				minLength = "0"
			}
			out.WriteString(" @len(" + minLength + ", " + maxLength + ")")
		}
	case strings.HasSuffix(expr, "[]"):
		if value, ok := number("minItems"); ok {
			out.WriteString(" @min_items(" + value + ")")
		}
		if value, ok := number("maxItems"); ok {
			out.WriteString(" @max_items(" + value + ")")
		}
	case isScalarExpr(expr) && expr != "bool" && expr != "timestamp" && expr != "json":
		if value, ok := number("minimum"); ok {
			if schema["exclusiveMinimum"] == true {
				out.WriteString(" @gt(" + value + ")")
			} else {
				out.WriteString(" @gte(" + value + ")")
			}
		} else if value, ok := number("exclusiveMinimum"); ok {
			out.WriteString(" @gt(" + value + ")")
		}
		if value, ok := number("maximum"); ok {
			if schema["exclusiveMaximum"] == true {
				out.WriteString(" @lt(" + value + ")")
			} else {
				out.WriteString(" @lte(" + value + ")")
			}
		} else if value, ok := number("exclusiveMaximum"); ok {
			out.WriteString(" @lt(" + value + ")")
		}
	}
	return out.String()
}

// --- refs ------------------------------------------------------------------

func (im *importer) deref(m map[string]any) map[string]any {
	for range 8 {
		ref := text(m, "$ref")
		if ref == "" {
			return m
		}
		resolved := im.lookupRef(ref)
		if resolved == nil {
			return m
		}
		m = resolved
	}
	return m
}

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
	if strings.HasPrefix(ref, "#/components/schemas/") && text(target, "type") == schemaObject && len(asMap(target["properties"])) > 0 {
		name := im.registerMessage(suggested)
		im.refDone[ref] = fieldType{expr: name}
		im.fillObject(name, target, depth+1)
		return im.refDone[ref], true
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

func (im *importer) fillObject(name string, schema map[string]any, depth int) {
	required := map[string]bool{}
	for _, r := range asSlice(schema["required"]) {
		required[textOf(r)] = true
	}
	im.messages[name] = im.convertProperties(name, asMap(schema["properties"]), required, depth+1)
}

// --- registry --------------------------------------------------------------

// registerMessage reserves a declaration slot and returns its (possibly
// disambiguated) name.
func (im *importer) declareReserved(name string) {
	if _, ok := im.messages[name]; ok {
		return
	}
	im.messages[name] = nil
	im.orderMsg = append(im.orderMsg, name)
}

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

func normalizeKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeKeys(item)
		}
		return typed
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprint(key)] = normalizeKeys(item)
		}
		return out
	case []any:
		for i, item := range typed {
			typed[i] = normalizeKeys(item)
		}
		return typed
	default:
		return value
	}
}
