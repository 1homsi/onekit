// Package onkimport converts OpenAPI 3.x documents into onekit schemas, which
// it renders as TypeSpec. It is intentionally opinionated: deterministic
// output, local $refs only, and warnings (never errors) for constructs
// without a faithful onekit mapping.
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
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktsp"
)

const maxDepth = 32

// Options tunes conversion.
type Options struct {
	// Package is the schema's namespace.
	Package string
	// Service is the generated service name (default: Api + "Service").
	Service string
}

// Result carries the converted schema plus non-fatal diagnostics.
type Result struct {
	// Source is the schema as TypeSpec.
	Source []byte
	// File holds the declarations Source spells.
	File     *onkschema.File
	Package  string
	Warnings []string
}

// importedMessage is a message under construction.
type importedMessage struct {
	fields     []*onkschema.FieldDecl
	decorators []onkschema.Decorator
}

type importer struct {
	root map[string]any
	opts Options

	warnings    []string
	messages    map[string]*importedMessage // nil while converting
	orderMsg    []string
	errorMsgs   map[string]bool
	errorStatus map[string]int
	enums       map[string][]onkschema.EnumValueDecl
	orderEnum   []string

	usedNames map[string]bool
	refDone   map[string]fieldType
	refActive map[string]bool
}

// Import converts an OpenAPI 3.x document (YAML or JSON) into a TypeSpec
// schema.
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
	serviceName := opts.Service
	if serviceName == "" {
		serviceName = Pascal(pkg) + "Service"
	}
	im := &importer{
		root:        root,
		opts:        Options{Package: pkg, Service: serviceName},
		messages:    map[string]*importedMessage{},
		errorMsgs:   map[string]bool{},
		errorStatus: map[string]int{},
		enums:       map[string][]onkschema.EnumValueDecl{},
		usedNames:   map[string]bool{},
		refDone:     map[string]fieldType{},
		refActive:   map[string]bool{},
	}
	service, err := im.convertPaths()
	if err != nil {
		return nil, err
	}
	file := im.file(service)
	source, err := render(file)
	if err != nil {
		return nil, err
	}
	return &Result{
		Source:   source,
		File:     file,
		Package:  pkg,
		Warnings: im.warnings,
	}, nil
}

// --- paths -----------------------------------------------------------------

var httpMethods = []string{"get", "put", "post", "delete", "patch"}

func (im *importer) convertPaths() (*onkschema.ServiceDecl, error) {
	basePath := "/"
	if server := firstMap(asSlice(im.root["servers"])); server != nil {
		if raw := text(server, "url"); raw != "" {
			basePath = urlPath(raw)
		}
	}
	if webhooks := asMap(im.root["webhooks"]); len(webhooks) > 0 {
		im.warnf("%d webhooks are not supported and were dropped", len(webhooks))
	}
	paths := asMap(im.root["paths"])
	keys := make([]string, 0, len(paths))
	for key := range paths {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var rpcs []*onkschema.RPCDecl
	for _, pathKey := range keys {
		item := asMap(paths[pathKey])
		if item == nil {
			continue
		}
		sharedParams := asSlice(item["parameters"])
		for _, method := range []string{"head", "options", "trace"} {
			if item[method] != nil {
				im.warnf("%s %s: %s operations are not supported and were dropped", strings.ToUpper(method), pathKey, strings.ToUpper(method))
			}
		}
		for _, method := range httpMethods {
			op := asMap(item[method])
			if op == nil {
				continue
			}
			if op["callbacks"] != nil {
				im.warnf("%s %s: callbacks are not supported and were dropped", strings.ToUpper(method), pathKey)
			}
			params := append(append([]any{}, sharedParams...), asSlice(op["parameters"])...)
			rpcs = append(rpcs, im.convertOperation(op, method, pathKey, params))
		}
	}
	if len(rpcs) == 0 {
		return nil, errors.New("document declares no operations")
	}
	return &onkschema.ServiceDecl{Name: im.opts.Service, BasePath: basePath, RPCs: rpcs}, nil
}

func (im *importer) convertOperation(op map[string]any, method, pathKey string, params []any) *onkschema.RPCDecl {
	opName := im.operationName(op, method, pathKey)

	reqName := im.registerMessage(opName + "Request")

	requestBody, hasBody := im.requestBodyFor(op, opName, method)
	pr := im.convertParameters(op, params, pathKey, reqName, opName, hasBody)
	pr.otherFields = append(pr.otherFields, im.undeclaredPathFields(pathKey, opName, &pr)...)
	pr.otherFields = append(pr.otherFields, im.requestBodyFields(requestBody, opName)...)
	im.messages[reqName] = &importedMessage{fields: append(pr.otherFields, pr.queryFields...)}

	return im.renderRPC(op, opName, reqName, method, pr, hasBody)
}

const (
	methodPatch = "patch"
	methodPost  = "post"
	methodPut   = "put"
)

func (im *importer) requestBodyFor(op map[string]any, opName, method string) (map[string]any, bool) {
	// @query binding is only legal on non-body verbs, so detect a request
	// body before emitting any parameters: query params on body-bearing
	// operations must fold into plain fields.
	requestBody := im.deref(asMap(op["requestBody"]))
	if requestBody != nil && method != methodPost && method != methodPut && method != methodPatch {
		im.warnf("%s: request body on %s is not supported by onekit and was dropped", opName, strings.ToUpper(method))
		requestBody = nil
	}
	hasBody := false
	if requestBody != nil {
		if _, ok := im.jsonSchema(asMap(requestBody["content"])); ok {
			hasBody = true
		}
	}
	return requestBody, hasBody
}

type operationParams struct {
	route       string
	boundPath   map[string]bool
	queryFields []*onkschema.FieldDecl
	otherFields []*onkschema.FieldDecl
	headers     []onkschema.HeaderDecl
	scopes      []string
}

func (im *importer) convertParameters(op map[string]any, params []any, pathKey, reqName, opName string, hasBody bool) operationParams {
	boundPath := map[string]bool{}
	var queryFields, otherFields []*onkschema.FieldDecl
	headers, scopes := im.securityHeaders(op, opName)
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
		decl := composeField(field, ft, optional)
		switch in {
		case paramInPath:
			if field != name {
				route = strings.ReplaceAll(route, "{"+name+"}", "{"+field+"}")
			}
			boundPath[name] = true
			otherFields = append(otherFields, decl)
		case "query":
			switch {
			case hasBody:
				im.warnf("%s: query parameter %q folded without binding (@query is reserved for non-body verbs)", opName, name)
			case name == field:
				decl.Decorators = append(decl.Decorators, dec("query"))
			default:
				decl.Decorators = append(decl.Decorators, dec("query", quoted(name)))
			}
			queryFields = append(queryFields, decl)
		}
	}
	return operationParams{route: route, boundPath: boundPath, queryFields: queryFields, otherFields: otherFields, headers: headers, scopes: scopes}
}

func (im *importer) undeclaredPathFields(pathKey, opName string, pr *operationParams) []*onkschema.FieldDecl {
	var fields []*onkschema.FieldDecl
	for _, name := range pathTemplateNames(pathKey) {
		if pr.boundPath[name] {
			continue
		}
		field := safeIdent(name)
		pr.route = strings.ReplaceAll(pr.route, "{"+name+"}", "{"+field+"}")
		fields = append(fields, composeField(field, fieldType{expr: scalarString}, false))
		im.warnf("%s: path parameter %q is not declared; imported as a string", opName, name)
	}
	return fields
}

func (im *importer) requestBodyFields(requestBody map[string]any, opName string) []*onkschema.FieldDecl {
	var fields []*onkschema.FieldDecl
	if requestBody != nil {
		if schema, ok := im.jsonSchema(asMap(requestBody["content"])); ok {
			ft, _ := im.schemaTypeExpr(schema, opName+"Body", 1)
			// @body is declared at RPC level; the field itself stays plain.
			fields = append(fields, composeField("body", ft, !truthy(requestBody["required"])))
		}
	}
	return fields
}

func (im *importer) renderRPC(op map[string]any, opName, reqName, method string, pr operationParams, hasBody bool) *onkschema.RPCDecl {
	respName, union, stream := im.responsePieces(opName, method, asMap(op["responses"]))
	rpc := &onkschema.RPCDecl{Name: opName, RequestType: reqName, ResponseType: respName, ErrorTypes: union, Headers: pr.headers}
	if hasBody {
		rpc.Decorators = append(rpc.Decorators, dec("body", quoted("body")))
	}
	rpc.Decorators = append(rpc.Decorators, dec(method, quoted(pr.route)))
	if stream {
		rpc.Decorators = append(rpc.Decorators, dec("stream"))
	}
	if len(pr.scopes) > 0 {
		scopes := make([]onkschema.Arg, 0, len(pr.scopes))
		for _, scope := range pr.scopes {
			scopes = append(scopes, quoted(scope))
		}
		rpc.Decorators = append(rpc.Decorators, dec("requires", scopes...))
	}
	return rpc
}

var implicitHeaders = map[string]bool{"accept": true, "content-type": true, "content-length": true}

func appendHeader(headers []onkschema.HeaderDecl, name string, decorators []onkschema.Decorator) []onkschema.HeaderDecl {
	if name == "" || implicitHeaders[strings.ToLower(name)] {
		return headers
	}
	for _, existing := range headers {
		if strings.EqualFold(existing.Name, name) {
			return headers
		}
	}
	return append(headers, onkschema.HeaderDecl{Name: name, Type: scalarString, Decorators: decorators})
}

func headerDecorators(param map[string]any) []onkschema.Decorator {
	var out []onkschema.Decorator
	if truthy(param["required"]) {
		out = append(out, dec("required"))
	}
	switch format := text(asMap(param["schema"]), "format"); format {
	case "uuid", "email", "uri":
		out = append(out, dec("format", quoted(format)))
	}
	if truthy(param["deprecated"]) {
		out = append(out, dec("deprecated"))
	}
	return out
}

func (im *importer) securityHeaders(op map[string]any, opName string) ([]onkschema.HeaderDecl, []string) {
	requirements, ok := op["security"]
	if !ok {
		requirements = im.root["security"]
	}
	options := asSlice(requirements)
	if len(options) == 0 {
		return nil, nil
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
		return nil, nil
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
	var headers []onkschema.HeaderDecl
	var scopes []string
	for _, name := range names {
		scheme := im.deref(asMap(schemes[name]))
		expressed := true
		switch kind := text(scheme, "type"); {
		case kind == "apiKey" && text(scheme, "in") == "header":
			headers = appendHeader(headers, text(scheme, "name"), authDecorators("api_key", name))
		case kind == "http" && strings.EqualFold(text(scheme, "scheme"), "bearer"):
			headers = appendHeader(headers, "Authorization", authDecorators("bearer", name))
		case kind == "http" && strings.EqualFold(text(scheme, "scheme"), "basic"):
			headers = appendHeader(headers, "Authorization", authDecorators("basic", name))
		default:
			expressed = false
			im.warnf("%s: security scheme %q (%s) cannot be expressed as a header and was dropped", opName, name, kind)
		}
		if expressed {
			scopes = im.appendScopes(scopes, asSlice(chosen[name]), opName)
		}
	}
	return headers, scopes
}

func (im *importer) appendScopes(scopes []string, raw []any, opName string) []string {
	for _, item := range raw {
		scope, ok := item.(string)
		if !ok || scope == "" {
			continue
		}
		if !validScope(scope) {
			im.warnf("%s: security scope %q contains characters @Onkt.requires does not allow and was dropped", opName, scope)
			continue
		}
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func validScope(scope string) bool {
	for _, r := range scope {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(":_.-/", r):
		default:
			return false
		}
	}
	return scope != ""
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
func (im *importer) responsePieces(opName, method string, responses map[string]any) (string, []string, bool) {
	respName := im.uniqueName(opName + "Response")
	var union []string

	successCode := pickSuccess(responses)
	schema, hasSchema := map[string]any{}, false
	stream := false
	if successCode != "" {
		content := asMap(im.deref(asMap(responses[successCode]))["content"])
		if eventSchema, ok := eventStreamSchema(content); ok && method != "post" && method != "put" && method != "patch" {
			schema, hasSchema, stream = eventSchema, true, true
		} else {
			schema, hasSchema = im.jsonSchema(content)
		}
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
			ft = fieldType{expr: scalarJSON}
		}
		switch {
		case im.isDecl(ft.expr):
			respName = ft.expr
		case strings.HasSuffix(ft.expr, "[]") || isMapExpr(ft.expr) ||
			isScalarExpr(ft.expr):
			im.declareReserved(respName)
			data := composeField("data", ft, false)
			data.Decorators = append(data.Decorators, dec("unwrap"))
			im.messages[respName] = &importedMessage{fields: []*onkschema.FieldDecl{data}}
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
			im.messages[errName] = &importedMessage{fields: []*onkschema.FieldDecl{composeField("message", fieldType{expr: scalarString}, false)}}
		}
		errName = im.errorWithStatus(errName, status)
		seenStatus[status] = true
		union = append(union, errName)
	}
	return respName, union, stream
}

func eventStreamSchema(content map[string]any) (map[string]any, bool) {
	for key, raw := range content {
		if strings.ToLower(strings.TrimSpace(strings.SplitN(key, ";", 2)[0])) != "text/event-stream" {
			continue
		}
		media := asMap(raw)
		if item := asMap(media["itemSchema"]); item != nil {
			data := asMap(asMap(item["properties"])["data"])
			if payload := asMap(data["contentSchema"]); payload != nil {
				return payload, true
			}
			if data != nil && (data["$ref"] != nil || text(data, "type") == schemaObject) {
				return data, true
			}
			continue
		}
		if schema := asMap(media["schema"]); schema != nil && (schema["$ref"] != nil || text(schema, "type") == schemaObject) {
			return schema, true
		}
	}
	return nil, false
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
		im.messages[alias] = &importedMessage{fields: append([]*onkschema.FieldDecl(nil), im.messages[name].fields...)}
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
	m := im.messages[name]
	if m == nil {
		m = &importedMessage{}
		im.messages[name] = m
	}
	m.decorators = append([]onkschema.Decorator{dec("status", num(strconv.Itoa(status)))}, m.decorators...)
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
		allowed := make([]onkschema.Arg, 0, len(values))
		for _, value := range values {
			allowed = append(allowed, quoted(textOf(value)))
		}
		return fieldType{expr: scalarString, decorators: []onkschema.Decorator{dec("in", allowed...)}}
	}
	ft, ok := im.schemaTypeExpr(schema, suggested, 1)
	if !ok || ft.expr == "" {
		return fieldType{expr: scalarString}
	}
	if isScalarExpr(ft.expr) && ft.expr != scalarTime && ft.expr != scalarJSON {
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

// fieldType describes an imported field's type: expr is a scalar or
// declaration name, "T[]" for a list, or "map<string, T>".
type fieldType struct {
	expr     string
	nullable bool
	// decorators are the validators and encodings the type carries.
	decorators []onkschema.Decorator
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
		return fieldType{expr: scalarJSON}, true
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
			// int64 items stay JSON numbers, as the document declares them.
			return fieldType{expr: scalarInt64 + "[]", decorators: []onkschema.Decorator{dec("encode", quoted("number"))}}, true
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
			return fieldType{expr: "map<string, " + value.expr + ">"}, true
		case len(props) == 0:
			return fieldType{expr: scalarJSON}, true
		}
		name := im.registerMessage(suggested)
		im.fillObject(name, schema, depth)
		return fieldType{expr: name}, true
	case "integer":
		if text(schema, "format") == "int64" {
			return fieldType{expr: scalarInt64, decorators: []onkschema.Decorator{dec("encode", quoted("number"))}}, true
		}
		return fieldType{expr: "int32"}, true
	case "number":
		return fieldType{expr: "float64"}, true
	case "boolean":
		return fieldType{expr: "bool"}, true
	case scalarString:
		return im.stringFieldType(schema, suggested), true
	default:
		im.warnf("schema %q uses unsupported composition (%s); mapped to json", suggested, strings.Join(mapKeys(schema), ","))
		return fieldType{expr: scalarJSON}, true
	}
}

func (im *importer) stringFieldType(schema map[string]any, suggested string) fieldType {
	switch text(schema, "format") {
	case "date-time":
		return fieldType{expr: scalarTime}
	case "date":
		return fieldType{expr: scalarTime, decorators: []onkschema.Decorator{dec("encode", quoted("date"))}}
	}
	if values := asSlice(schema["enum"]); len(values) > 0 {
		// Enums live in their own registry: registering through the message
		// table would emit a stray empty message sharing the name.
		base := suggested
		if !strings.HasSuffix(base, "Values") {
			base += "Values"
		}
		name := im.uniqueName(base)
		var members []onkschema.EnumValueDecl
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
			value := onkschema.EnumValueDecl{Name: member}
			if member != raw {
				value.Decorators = []onkschema.Decorator{dec("json", quoted(raw))}
			}
			members = append(members, value)
		}
		if len(members) == 0 {
			return fieldType{expr: "string"}
		}
		im.enums[name] = members
		im.orderEnum = append(im.orderEnum, name)
		return fieldType{expr: name}
	}
	ft := fieldType{expr: scalarString}
	switch format := text(schema, "format"); format {
	case "email", "uuid", "uri":
		ft.decorators = []onkschema.Decorator{dec(format)}
	}
	return ft
}

func (im *importer) convertProperties(msgName string, props map[string]any, required map[string]bool, depth int) []*onkschema.FieldDecl {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]*onkschema.FieldDecl, 0, len(names))
	final := map[string]string{}
	claimed := map[string]bool{}
	for _, pass := range []bool{true, false} {
		for _, rawName := range names {
			field := safeIdent(rawName)
			if (field == rawName) != pass {
				continue
			}
			base := field
			for i := 2; claimed[fieldKey(field)]; i++ {
				field = base + "_" + strconv.Itoa(i)
			}
			claimed[fieldKey(field)] = true
			final[rawName] = field
		}
	}
	for _, rawName := range names {
		field := final[rawName]
		if field != rawName {
			im.warnf("%s.%q renamed to %q", msgName, rawName, field)
		}
		ft, ok := im.schemaTypeExpr(props[rawName], msgName+Pascal(field), depth)
		if !ok || ft.expr == "" {
			im.warnf("%s.%q has no convertible schema; mapped to json", msgName, rawName)
			ft = fieldType{expr: scalarJSON}
		}
		ft.decorators = append(ft.decorators, im.constraintDecorators(im.derefSchema(asMap(props[rawName])), ft.expr)...)
		decl := composeField(field, ft, !required[rawName])
		decl.Doc = docText(text(asMap(props[rawName]), "description"))
		fields = append(fields, decl)
	}
	return fields
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

// docText trims every line of an OpenAPI description.
func docText(doc string) string {
	if strings.TrimSpace(doc) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(doc), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

func pathTemplateNames(path string) []string {
	var names []string
	for rest := path; ; {
		start := strings.Index(rest, "{")
		if start < 0 {
			return names
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return names
		}
		names = append(names, rest[start+1:start+end])
		rest = rest[start+end+1:]
	}
}

func orderedBounds(low, high string) bool {
	a, errA := strconv.ParseFloat(low, 64)
	b, errB := strconv.ParseFloat(high, 64)
	return errA == nil && errB == nil && a <= b
}

func fieldKey(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (im *importer) constraintDecorators(schema map[string]any, expr string) []onkschema.Decorator {
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
	var out []onkschema.Decorator
	switch {
	case expr == scalarString:
		if pattern := text(schema, "pattern"); pattern != "" {
			im.warnf("string pattern %q is not imported; add @pattern by hand if the field needs it", pattern)
		}
		minLength, hasMin := number("minLength")
		maxLength, hasMax := number("maxLength")
		if !hasMin {
			minLength = "0"
		}
		if !hasMax {
			maxLength = strconv.Itoa(math.MaxInt32)
		}
		switch {
		case !hasMin && !hasMax:
		case orderedBounds(minLength, maxLength):
			out = append(out, dec("len", num(minLength), num(maxLength)))
		default:
			im.warnf("minLength %s is greater than maxLength %s; length constraint dropped", minLength, maxLength)
		}
	case strings.HasSuffix(expr, "[]"):
		minItems, hasMin := number("minItems")
		maxItems, hasMax := number("maxItems")
		if hasMin && hasMax && !orderedBounds(minItems, maxItems) {
			im.warnf("minItems %s is greater than maxItems %s; item constraints dropped", minItems, maxItems)
			break
		}
		if hasMin {
			out = append(out, dec("min_items", num(minItems)))
		}
		if hasMax {
			out = append(out, dec("max_items", num(maxItems)))
		}
	case isScalarExpr(expr) && expr != "bool" && expr != scalarTime && expr != scalarJSON:
		if value, ok := number("minimum"); ok {
			if schema["exclusiveMinimum"] == true {
				out = append(out, dec("gt", num(value)))
			} else {
				out = append(out, dec("gte", num(value)))
			}
		} else if value, ok := number("exclusiveMinimum"); ok {
			out = append(out, dec("gt", num(value)))
		}
		if value, ok := number("maximum"); ok {
			if schema["exclusiveMaximum"] == true {
				out = append(out, dec("lt", num(value)))
			} else {
				out = append(out, dec("lte", num(value)))
			}
		} else if value, ok := number("exclusiveMaximum"); ok {
			out = append(out, dec("lt", num(value)))
		}
	}
	return out
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
		return fieldType{expr: scalarJSON}, true
	}
	target := im.lookupRef(ref)
	if target == nil {
		return fieldType{expr: scalarJSON}, true
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
		result = fieldType{expr: scalarJSON}
	}
	im.refDone[ref] = result
	return result, true
}

func (im *importer) fillObject(name string, schema map[string]any, depth int) {
	required := map[string]bool{}
	for _, r := range asSlice(schema["required"]) {
		required[textOf(r)] = true
	}
	im.messages[name] = &importedMessage{fields: im.convertProperties(name, asMap(schema["properties"]), required, depth+1)}
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

// --- output ----------------------------------------------------------------

// importHeader marks a schema as generated by onkt import.
const importHeader = "// Code generated by onkt import; re-run import instead of editing."

// file assembles the declarations: plain messages, then enums, then error
// messages, each group by name, then the service.
func (im *importer) file(service *onkschema.ServiceDecl) *onkschema.File {
	file := &onkschema.File{Package: im.opts.Package, LeadingComments: []string{importHeader}}
	msgNames := sortUnique(im.orderMsg)
	message := func(name string) *onkschema.MessageDecl {
		decl := &onkschema.MessageDecl{Name: name}
		if m := im.messages[name]; m != nil {
			decl.Fields = m.fields
			decl.Decorators = m.decorators
		}
		return decl
	}
	for _, name := range msgNames {
		if !im.errorMsgs[name] {
			file.Messages = append(file.Messages, message(name))
		}
	}
	for _, name := range sortUnique(im.orderEnum) {
		file.Enums = append(file.Enums, &onkschema.EnumDecl{Name: name, Values: im.enums[name]})
	}
	for _, name := range msgNames {
		if im.errorMsgs[name] {
			file.Messages = append(file.Messages, message(name))
		}
	}
	file.Services = []*onkschema.ServiceDecl{service}
	for _, m := range file.Messages {
		if !im.errorMsgs[m.Name] {
			file.Declarations = append(file.Declarations, m)
		}
	}
	for _, e := range file.Enums {
		file.Declarations = append(file.Declarations, e)
	}
	for _, m := range file.Messages {
		if im.errorMsgs[m.Name] {
			file.Declarations = append(file.Declarations, m)
		}
	}
	file.Declarations = append(file.Declarations, service)
	return file
}

// render spells the declarations as TypeSpec.
func render(file *onkschema.File) ([]byte, error) {
	out, err := onktsp.Convert([]onktsp.Source{{Path: file.Package + ".tsp", AST: file}})
	if err != nil {
		return nil, fmt.Errorf("render the imported schema: %w", err)
	}
	return []byte(out[0].Content), nil
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
