package onkcompile

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const (
	flattenDecorator = "flatten"
	postVerb         = "post"
)

// validateSyntax rejects decorators and RPC declarations that the generators
// cannot faithfully implement. Keeping this in the compiler makes every
// generator share one contract instead of each backend silently ignoring a
// spelling mistake or unsupported combination.
func validateSyntax(sources []Source, options CompileOptions) error {
	seenServices := map[string]string{}
	seenRoutes := map[string]string{}
	for _, src := range sources {
		for _, message := range src.AST.Messages {
			if err := validateMessageDecl(src.Path, message, options); err != nil {
				return err
			}
		}
		for _, enum := range src.AST.Enums {
			if err := validateEnumDecl(src.Path, enum, options); err != nil {
				return err
			}
		}
		for _, service := range src.AST.Services {
			serviceKey := src.AST.Package + "." + service.Name
			if previous, exists := seenServices[serviceKey]; exists {
				return &Error{Path: src.Path, Line: service.Line, Msg: fmt.Sprintf(
					"duplicate service name %q (already declared in %s)", service.Name, previous,
				)}
			}
			seenServices[serviceKey] = src.Path
			if err := validateServiceDecl(src.Path, service, seenRoutes, options); err != nil {
				return err
			}
		}
	}
	return nil
}

type decoratorRule struct {
	minArgs int
	maxArgs int
}

var (
	messageDecorators = map[string]decoratorRule{
		"status": {minArgs: 1, maxArgs: 1},
	}
	fieldDecorators = map[string]decoratorRule{
		"email": {}, "uuid": {}, "uri": {}, "required": {}, "unwrap": {},
		"len": {minArgs: 2, maxArgs: 2}, "range": {minArgs: 2, maxArgs: 2},
		"in": {minArgs: 1, maxArgs: -1}, "pattern": {minArgs: 1, maxArgs: 1},
		"gt": {minArgs: 1, maxArgs: 1}, "gte": {minArgs: 1, maxArgs: 1},
		"lt": {minArgs: 1, maxArgs: 1}, "lte": {minArgs: 1, maxArgs: 1},
		"min_items": {minArgs: 1, maxArgs: 1}, "max_items": {minArgs: 1, maxArgs: 1},
		flattenDecorator: {minArgs: 0, maxArgs: 1}, "encode": {minArgs: 1, maxArgs: 1},
		"empty": {minArgs: 1, maxArgs: 1}, "query": {minArgs: 0, maxArgs: 1},
	}
	headerDecorators = map[string]decoratorRule{
		"required": {}, "format": {minArgs: 1, maxArgs: 1}, "example": {minArgs: 1, maxArgs: 1},
		"deprecated": {maxArgs: 1}, "auth": {minArgs: 1, maxArgs: 1}, "auth_scheme_name": {minArgs: 1, maxArgs: 1},
	}
	enumValueDecorators = map[string]decoratorRule{"json": {minArgs: 1, maxArgs: 1}}
	variantDecorators   = map[string]decoratorRule{"tag": {minArgs: 1, maxArgs: 1}, "json": {minArgs: 1, maxArgs: 1}}
)

func validateMessageDecl(path string, message *onklang.MessageDecl, options CompileOptions) error {
	if err := validateDeclarationName(path, message.Line, message.Name); err != nil {
		return err
	}
	if err := validateDecorators(path, message.Line, message.Decorators, messageDecorators); err != nil {
		return err
	}
	seenFields := map[string]string{}
	unwrapCount := 0
	for _, field := range message.Fields {
		if !options.AllowLegacyContracts {
			if err := validateMemberName(path, field.Line, field.Name); err != nil {
				return err
			}
		}
		generated := generatedIdentifier(field.Name)
		if previous, exists := seenFields[generated]; exists {
			return &Error{Path: path, Line: field.Line, Msg: fmt.Sprintf(
				"field name %q collides with %q after target-language name conversion", field.Name, previous,
			)}
		}
		seenFields[generated] = field.Name
		if err := validateFieldDecl(path, field, options); err != nil {
			return err
		}
		if hasDecorator(field.Decorators, "unwrap") {
			unwrapCount++
		}
	}
	if unwrapCount > 0 && (unwrapCount != 1 || len(message.Fields) != 1) {
		return &Error{Path: path, Line: message.Line, Msg: "@unwrap requires a message with exactly one field"}
	}
	for _, nested := range message.Nested {
		if err := validateMessageDecl(path, nested, options); err != nil {
			return err
		}
	}
	for _, enum := range message.NestedEn {
		if err := validateEnumDecl(path, enum, options); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldDecl(path string, field *onklang.FieldDecl, options CompileOptions) error {
	if err := validateDecorators(path, field.Line, field.Decorators, fieldDecorators); err != nil {
		return err
	}
	if field.Oneof == nil {
		return validateFieldDecoratorSemantics(path, field, options)
	}
	if err := validateOneofArgs(path, field.Line, field.Oneof.Args); err != nil {
		return err
	}
	seenNames := map[string]string{}
	seenTags := map[string]string{}
	for _, variant := range field.Oneof.Variants {
		if err := validateDecorators(path, variant.Line, variant.Decorators, variantDecorators); err != nil {
			return err
		}
		generated := generatedIdentifier(variant.Name)
		if previous, exists := seenNames[generated]; exists {
			return &Error{Path: path, Line: variant.Line, Msg: fmt.Sprintf(
				"oneof variant %q collides with %q after target-language name conversion", variant.Name, previous,
			)}
		}
		seenNames[generated] = variant.Name
		tag := variant.Name
		if decorator, ok := findDecorator(variant.Decorators, "tag"); ok {
			tag = decorator.Args[0].Value
		}
		if previous, exists := seenTags[tag]; exists {
			return &Error{Path: path, Line: variant.Line, Msg: fmt.Sprintf(
				"duplicate oneof tag %q on %q and %q", tag, previous, variant.Name,
			)}
		}
		seenTags[tag] = variant.Name
	}
	return nil
}

func validateEnumDecl(path string, enum *onklang.EnumDecl, options CompileOptions) error {
	if err := validateDeclarationName(path, enum.Line, enum.Name); err != nil {
		return err
	}
	seenNames := map[string]string{}
	seenJSON := map[string]string{}
	for _, value := range enum.Values {
		if !options.AllowLegacyContracts {
			if err := validateMemberName(path, value.Line, value.Name); err != nil {
				return err
			}
		}
		if err := validateDecorators(path, value.Line, value.Decorators, enumValueDecorators); err != nil {
			return err
		}
		generated := generatedIdentifier(value.Name)
		if previous, exists := seenNames[generated]; exists {
			return &Error{Path: path, Line: value.Line, Msg: fmt.Sprintf(
				"enum value %q collides with %q after target-language name conversion", value.Name, previous,
			)}
		}
		seenNames[generated] = value.Name
		jsonName := value.Name
		if decorator, ok := findDecorator(value.Decorators, "json"); ok {
			jsonName = decorator.Args[0].Value
		}
		if previous, exists := seenJSON[jsonName]; exists {
			return &Error{Path: path, Line: value.Line, Msg: fmt.Sprintf(
				"duplicate enum JSON value %q on %q and %q", jsonName, previous, value.Name,
			)}
		}
		seenJSON[jsonName] = value.Name
	}
	return nil
}

func validateOneofArgs(path string, line int, args []onklang.Arg) error {
	seen := map[string]bool{}
	for _, arg := range args {
		if arg.Name != "discriminator" && arg.Name != flattenDecorator {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("unknown oneof argument %q", arg.Name)}
		}
		if seen[arg.Name] {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("duplicate oneof argument %q", arg.Name)}
		}
		seen[arg.Name] = true
		if arg.Name == "discriminator" && arg.Value == "" {
			return &Error{Path: path, Line: line, Msg: "oneof discriminator must not be empty"}
		}
		if arg.Name == flattenDecorator && arg.Value != "true" && arg.Value != "false" {
			return &Error{Path: path, Line: line, Msg: "oneof flatten must be true or false"}
		}
	}
	return nil
}

func validateFieldDecoratorSemantics(filePath string, field *onklang.FieldDecl, options CompileOptions) error {
	if field.Type == nil {
		return nil
	}
	if hasDecorator(field.Decorators, "nullable") {
		return &Error{Path: filePath, Line: field.Line, Msg: "@nullable is unsupported; use the ? optional marker"}
	}
	if field.Repeated && hasDecorator(field.Decorators, "query") {
		return &Error{Path: filePath, Line: field.Line, Msg: "@query currently supports only non-repeated scalar fields"}
	}
	if hasDecorator(field.Decorators, "query") && !isScalarTypeRef(field.Type) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@query requires a scalar field"}
	}
	for _, decorator := range field.Decorators {
		switch decorator.Name {
		case "email", "uuid", "uri", "pattern", "len", "in":
			if !isScalarNamed(field.Type, "string") || field.Repeated {
				return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated string field", decorator.Name)}
			}
		case "gt", "gte", "lt", "lte", "range":
			if !isNumericTypeRef(field.Type) || field.Repeated {
				return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated numeric field", decorator.Name)}
			}
		case "min_items", "max_items":
			if !field.Repeated {
				return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a repeated field", decorator.Name)}
			}
		case flattenDecorator, "empty":
			if field.Repeated || field.Type.IsMap || isScalarTypeRef(field.Type) {
				return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated message field", decorator.Name)}
			}
		case "unwrap":
			if field.Optional {
				return &Error{Path: filePath, Line: field.Line, Msg: "@unwrap cannot be optional"}
			}
		case "encode":
			if field.Repeated || field.Type.IsMap {
				return &Error{Path: filePath, Line: field.Line, Msg: "@encode does not support repeated or map fields"}
			}
		}
		if err := validateDecoratorValue(filePath, field.Line, decorator); err != nil {
			return err
		}
	}
	if !options.AllowLegacyContracts && hasDecorator(field.Decorators, "required") && !field.Optional && isScalarTypeRef(field.Type) &&
		!isScalarNamed(field.Type, "string") {
		return &Error{Path: filePath, Line: field.Line, Msg: "@required on non-string scalars needs the ? marker so generators can track presence"}
	}
	return nil
}

func validateDecoratorValue(filePath string, line int, decorator onklang.Decorator) error {
	value := func(index int) string { return decorator.Args[index].Value }
	numeric := func(index int) error {
		if _, err := strconv.ParseFloat(value(index), 64); err != nil {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be numeric", decorator.Name, value(index))}
		}
		return nil
	}
	switch decorator.Name {
	case "len", "range":
		if err := numeric(0); err != nil {
			return err
		}
		if err := numeric(1); err != nil {
			return err
		}
		minimum, _ := strconv.ParseFloat(value(0), 64)
		maximum, _ := strconv.ParseFloat(value(1), 64)
		if minimum > maximum {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s minimum must not exceed maximum", decorator.Name)}
		}
	case "gt", "gte", "lt", "lte", "min_items", "max_items":
		if err := numeric(0); err != nil {
			return err
		}
	case "pattern":
		if _, err := regexp.Compile(value(0)); err != nil {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("invalid @pattern regular expression: %v", err)}
		}
	case "empty":
		if value(0) != "null" && value(0) != "omit" && value(0) != "preserve" {
			return &Error{Path: filePath, Line: line, Msg: "@empty must be null, omit, or preserve"}
		}
	case "encode":
		allowed := map[string]bool{
			"number": true, "hex": true, "base64": true, "base64_raw": true,
			"base64url": true, "base64url_raw": true, "unix_seconds": true,
			"unix_millis": true, "date": true,
		}
		if !allowed[value(0)] {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("unsupported @encode value %q", value(0))}
		}
	}
	return nil
}

func isScalarNamed(typ *onklang.TypeRef, name string) bool {
	return typ != nil && !typ.IsMap && typ.Name == name
}

func isScalarTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	ok := map[string]bool{
		"string": true, "bool": true, "int32": true, "int64": true, "uint32": true,
		"uint64": true, "float32": true, "float64": true, "bytes": true, "timestamp": true,
	}[typ.Name]
	return ok
}

func isNumericTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	return map[string]bool{
		"int32": true, "int64": true, "uint32": true, "uint64": true, "float32": true, "float64": true,
	}[typ.Name]
}

func validateServiceDecl(filePath string, service *onklang.ServiceDecl, seenRoutes map[string]string, options CompileOptions) error {
	if err := validateDeclarationName(filePath, service.Line, service.Name); err != nil {
		return err
	}
	if err := validateHTTPPath(service.BasePath, true); err != nil {
		return &Error{Path: filePath, Line: service.Line, Msg: "invalid service base_path: " + err.Error()}
	}
	if err := validateHeaders(filePath, service.Headers); err != nil {
		return err
	}
	seenMethods := map[string]string{}
	for _, rpc := range service.RPCs {
		if !options.AllowLegacyContracts {
			if err := validateMemberName(filePath, rpc.Line, rpc.Name); err != nil {
				return err
			}
		}
		generated := generatedIdentifier(rpc.Name)
		if previous, exists := seenMethods[generated]; exists {
			return &Error{Path: filePath, Line: rpc.Line, Msg: fmt.Sprintf(
				"RPC name %q collides with %q after target-language name conversion", rpc.Name, previous,
			)}
		}
		seenMethods[generated] = rpc.Name
		verb, route, err := validateRPC(filePath, rpc)
		if err != nil {
			return err
		}
		key := strings.ToUpper(verb) + " " + service.BasePath + route
		if previous, exists := seenRoutes[key]; exists {
			return &Error{Path: filePath, Line: rpc.Line, Msg: fmt.Sprintf(
				"duplicate HTTP route %s (already declared by %s)", key, previous,
			)}
		}
		seenRoutes[key] = service.Name + "." + rpc.Name
		if err := validateHeaders(filePath, rpc.Headers); err != nil {
			return err
		}
		serviceHeaders := map[string]bool{}
		for _, header := range service.Headers {
			serviceHeaders[strings.ToLower(header.Name)] = true
		}
		for _, header := range rpc.Headers {
			if serviceHeaders[strings.ToLower(header.Name)] {
				return &Error{Path: filePath, Line: header.Line, Msg: fmt.Sprintf(
					"RPC header %q conflicts with a service header", header.Name,
				)}
			}
		}
	}
	return nil
}

func validateHeaders(path string, headers []onklang.HeaderDecl) error {
	seen := map[string]bool{}
	for _, header := range headers {
		key := strings.ToLower(header.Name)
		if key == "" {
			return &Error{Path: path, Line: header.Line, Msg: "header name must not be empty"}
		}
		if seen[key] {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("duplicate header %q", header.Name)}
		}
		seen[key] = true
		if err := validateDecorators(path, header.Line, header.Decorators, headerDecorators); err != nil {
			return err
		}
		if kind, recognized := onkir.ParseScalarKind(header.Type); recognized && kind != onkir.ScalarString {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("header %q must use string type", header.Name)}
		}
		auth, hasAuth := findDecorator(header.Decorators, "auth")
		_, hasSchemeName := findDecorator(header.Decorators, "auth_scheme_name")
		if hasSchemeName && !hasAuth {
			return &Error{Path: path, Line: header.Line, Msg: "@auth_scheme_name requires @auth"}
		}
		if hasAuth && !hasDecorator(header.Decorators, "required") {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("@auth(%s) header must also be @required", auth.Args[0].Value)}
		}
		if format, ok := findDecorator(header.Decorators, "format"); ok {
			value := format.Args[0].Value
			if value != "uuid" && value != "email" && value != "uri" {
				return &Error{Path: path, Line: header.Line, Msg: "header @format must be uuid, email, or uri"}
			}
		}
	}
	return nil
}

func validateRPC(path string, rpc *onklang.RPCDecl) (string, string, error) {
	var verb, route string
	for _, decorator := range rpc.Decorators {
		if isHTTPVerb(decorator.Name) {
			if verb != "" {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "RPC must declare exactly one HTTP verb"}
			}
			if len(decorator.Args) != 1 || decorator.Args[0].Value == "" {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("@%s requires one non-empty route", decorator.Name)}
			}
			verb, route = decorator.Name, decorator.Args[0].Value
			continue
		}
		switch decorator.Name {
		case "stream":
			if len(decorator.Args) != 0 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@stream does not take arguments"}
			}
		case "body":
			if len(decorator.Args) > 1 {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body accepts at most one argument"}
			}
		default:
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: fmt.Sprintf("unknown RPC decorator @%s", decorator.Name)}
		}
	}
	if verb == "" {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "RPC must declare one HTTP verb"}
	}
	if err := validateHTTPPath(route, false); err != nil {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "invalid RPC route: " + err.Error()}
	}
	if body, ok := findDecorator(rpc.Decorators, "body"); ok {
		if verb != postVerb && verb != "put" && verb != "patch" && verb != "query" {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires a body-bearing HTTP verb"}
		}
		if len(body.Args) != 1 || body.Args[0].Value == "" {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires one non-empty request field name"}
		}
	}
	return verb, route, nil
}

func validateHTTPPath(value string, allowEmpty bool) error {
	if value == "" && allowEmpty {
		return nil
	}
	if !strings.HasPrefix(value, "/") {
		return errors.New("must start with /")
	}
	if strings.ContainsAny(value, "?#%") || strings.Contains(value, "//") {
		return errors.New("must be a canonical literal URL path")
	}
	withoutParams := regexp.MustCompile(`\{[^{}]+\}`).ReplaceAllString(value, "x")
	if path.Clean(withoutParams) != withoutParams {
		return errors.New("must be a canonical literal URL path")
	}
	if strings.Count(value, "{") != strings.Count(value, "}") {
		return errors.New("contains unbalanced path parameter braces")
	}
	for _, name := range pathParameterNames(value) {
		if name == "" || generatedIdentifier(name) != strings.ToLower(strings.ReplaceAll(name, "_", "")) {
			return fmt.Errorf("contains invalid path parameter %q", name)
		}
	}
	return nil
}

func isHTTPVerb(name string) bool {
	switch name {
	case "get", postVerb, "put", "delete", "patch", "query":
		return true
	default:
		return false
	}
}

func validateDecorators(path string, line int, decorators []onklang.Decorator, rules map[string]decoratorRule) error {
	seen := map[string]bool{}
	for _, decorator := range decorators {
		rule, ok := rules[decorator.Name]
		if !ok {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("unknown decorator @%s", decorator.Name)}
		}
		if seen[decorator.Name] {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("duplicate decorator @%s", decorator.Name)}
		}
		seen[decorator.Name] = true
		if len(decorator.Args) < rule.minArgs || (rule.maxArgs >= 0 && len(decorator.Args) > rule.maxArgs) {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("@%s expects %s", decorator.Name, argCount(rule.minArgs, rule.maxArgs))}
		}
		for _, arg := range decorator.Args {
			if arg.Name != "" && (decorator.Name != flattenDecorator || arg.Name != "prefix") {
				return &Error{Path: path, Line: line, Msg: fmt.Sprintf("@%s does not accept named argument %q", decorator.Name, arg.Name)}
			}
			if arg.Value == "" && decorator.Name != flattenDecorator {
				return &Error{Path: path, Line: line, Msg: fmt.Sprintf("@%s arguments must not be empty", decorator.Name)}
			}
		}
		switch decorator.Name {
		case "status":
			status, err := strconv.Atoi(decorator.Args[0].Value)
			if err != nil || status < 400 || status > 599 {
				return &Error{Path: path, Line: line, Msg: "@status must be an HTTP error status from 400 to 599"}
			}
		case "auth":
			value := decorator.Args[0].Value
			if value != "api_key" && value != "bearer" && value != "basic" {
				return &Error{Path: path, Line: line, Msg: "@auth must be api_key, bearer, or basic"}
			}
		case flattenDecorator:
			if len(decorator.Args) == 1 && decorator.Args[0].Name != "prefix" {
				return &Error{Path: path, Line: line, Msg: "@flatten argument must be named prefix"}
			}
		}
	}
	return nil
}

func generatedIdentifier(value string) string {
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return strings.ToLower(out.String())
}

var reservedDeclarationNames = map[string]bool{
	"break": true, "case": true, "chan": true, "class": true, "const": true, "continue": true,
	"default": true, "defer": true, "delete": true, "else": true, "enum": true, "export": true,
	"extends": true, "fallthrough": true, "false": true, "finally": true, "fn": true, "for": true,
	"from": true, "func": true, "go": true, "goto": true, "if": true, "implements": true,
	"import": true, "in": true, "interface": true, "let": true, "map": true, "match": true,
	"new": true, "nil": true, "none": true, "package": true, "pass": true, "range": true,
	"return": true, "select": true, "struct": true, "super": true, "switch": true, "trait": true,
	"true": true, "try": true, "type": true, "var": true, "while": true, "with": true, "yield": true,
}

var pythonMemberKeywords = map[string]bool{
	"and": true, "as": true, "assert": true, "async": true, "await": true, "break": true,
	"class": true, "continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "false": true, "finally": true, "for": true, "from": true, "global": true,
	"if": true, "import": true, "in": true, "is": true, "lambda": true, "none": true,
	"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true, "return": true,
	"true": true, "try": true, "while": true, "with": true, "yield": true,
}

func validateDeclarationName(path string, line int, name string) error {
	if reservedDeclarationNames[strings.ToLower(name)] {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("declaration name %q is reserved in a generated target language", name)}
	}
	return nil
}

func validateMemberName(path string, line int, name string) error {
	if pythonMemberKeywords[strings.ToLower(name)] {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("member name %q is reserved in Python", name)}
	}
	return nil
}

func hasDecorator(decorators []onklang.Decorator, name string) bool {
	_, ok := findDecorator(decorators, name)
	return ok
}

func findDecorator(decorators []onklang.Decorator, name string) (onklang.Decorator, bool) {
	for _, decorator := range decorators {
		if decorator.Name == name {
			return decorator, true
		}
	}
	return onklang.Decorator{}, false
}

func pathParameterNames(route string) []string {
	var names []string
	for start := strings.IndexByte(route, '{'); start >= 0; start = strings.IndexByte(route, '{') {
		route = route[start+1:]
		end := strings.IndexByte(route, '}')
		if end < 0 {
			break
		}
		names = append(names, route[:end])
		route = route[end+1:]
	}
	return names
}

func validateContract(pkg *onkir.Package) error {
	fullNames := map[string]string{}
	for _, file := range pkg.Files {
		for _, message := range file.Messages {
			if err := validateCompiledMessage(file.Path, message, fullNames); err != nil {
				return err
			}
		}
		for _, enum := range file.Enums {
			if file.Package != "" {
				if previous, exists := fullNames[enum.FullName()]; exists {
					return &Error{Path: file.Path, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", enum.FullName(), previous)}
				}
				fullNames[enum.FullName()] = file.Path
			}
		}
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if err := validateMethodBindings(file.Path, method); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateCompiledMessage(filePath string, message *onkir.Message, fullNames map[string]string) error {
	if message.File != nil && message.File.Package != "" {
		if previous, exists := fullNames[message.FullName()]; exists {
			return &Error{Path: filePath, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", message.FullName(), previous)}
		}
		fullNames[message.FullName()] = filePath
	}
	for _, field := range message.Fields {
		if err := validateCompiledField(filePath, field); err != nil {
			return err
		}
	}
	for _, nested := range message.Nested {
		if err := validateCompiledMessage(filePath, nested, fullNames); err != nil {
			return err
		}
	}
	for _, enum := range message.NestedEnums {
		if enum.File != nil && enum.File.Package != "" {
			if previous, exists := fullNames[enum.FullName()]; exists {
				return &Error{Path: filePath, Msg: fmt.Sprintf("qualified declaration %q conflicts with %s", enum.FullName(), previous)}
			}
			fullNames[enum.FullName()] = filePath
		}
	}
	return nil
}

func validateCompiledField(filePath string, field *onkir.Field) error {
	decorator, hasEncode := field.Decorator("encode")
	if hasEncode {
		value, _ := decorator.Value()
		valid := false
		if field.Type != nil {
			switch field.Type.Kind {
			case onkir.KindEnum:
				valid = value == "number"
			case onkir.KindScalar:
				switch field.Type.Scalar {
				case onkir.ScalarInt64, onkir.ScalarUint64:
					valid = value == "number"
				case onkir.ScalarBytes:
					valid = value == "hex" || value == "base64" || value == "base64_raw" || value == "base64url" || value == "base64url_raw"
				case onkir.ScalarTimestamp:
					valid = value == "unix_seconds" || value == "unix_millis" || value == "date"
				default:
					valid = false
				}
			}
		}
		if !valid {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@encode(%s) is not supported on field %s.%s", value, field.Message.FullName(), field.Name)}
		}
	}
	return nil
}

func validateMethodBindings(filePath string, method *onkir.Method) error {
	route, _ := method.Path()
	seenPath := map[string]bool{}
	for _, name := range pathParameterNames(route) {
		if seenPath[name] {
			return &Error{Path: filePath, Msg: fmt.Sprintf("duplicate path parameter %q on RPC %s", name, method.Name)}
		}
		seenPath[name] = true
		field := methodField(method.Request, name)
		if field == nil || field.Type == nil || field.Type.Kind != onkir.KindScalar || field.Repeated {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s requires one non-repeated scalar request field", name, method.Name)}
		}
	}
	seenQuery := map[string]string{}
	for _, field := range method.Request.Fields {
		decorator, ok := field.Decorator("query")
		if !ok {
			continue
		}
		name, _ := decorator.Value()
		if name == "" {
			name = field.Name
		}
		if previous, exists := seenQuery[name]; exists {
			return &Error{Path: filePath, Msg: fmt.Sprintf("query parameter %q is bound by both %s and %s", name, previous, field.Name)}
		}
		seenQuery[name] = field.Name
	}
	if bodyName, ok := method.BodyField(); ok {
		field := methodField(method.Request, bodyName)
		if field == nil {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@body references unknown request field %q on RPC %s", bodyName, method.Name)}
		}
		if seenPath[field.Name] {
			return &Error{Path: filePath, Msg: fmt.Sprintf("request field %q cannot be both a path and body binding", field.Name)}
		}
		if _, query := field.Decorator("query"); query {
			return &Error{Path: filePath, Msg: fmt.Sprintf("request field %q cannot be both a query and body binding", field.Name)}
		}
	}
	return nil
}

func methodField(message *onkir.Message, name string) *onkir.Field {
	for _, field := range message.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

func argCount(minimum, maximum int) string {
	if minimum == maximum {
		return strconv.Itoa(minimum) + " argument(s)"
	}
	if maximum < 0 {
		return "at least " + strconv.Itoa(minimum) + " argument(s)"
	}
	return strconv.Itoa(minimum) + " to " + strconv.Itoa(maximum) + " argument(s)"
}
