package onkcompile

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const (
	flattenDecorator            = "flatten"
	flattenPrefixArg            = "prefix"
	maxCrossRuntimePatternBytes = 4096
	oneofDiscriminatorArg       = "discriminator"
	postVerb                    = "post"
	putVerb                     = "put"
	patchVerb                   = "patch"
	queryVerb                   = "query"
	// wsDecorator binds an RPC as a bidirectional WebSocket stream; its
	// argument is the upgrade route.
	wsDecorator = "ws"
	// wsTransport is the pseudo-verb recorded for WebSocket RPCs in route
	// uniqueness checks and compatibility signatures.
	wsTransport = "ws"
	// wsIDDecorator marks a field as the correlation key for matching an
	// outbound @ws frame (or oneof variant of one) to its eventual inbound
	// reply on the same connection, so generators can emit a pending-call
	// map instead of leaving multiplexing to hand-rolled application code.
	wsIDDecorator = "ws_id"
	// wsCancelDecorator marks the oneof variant sent to abandon a correlated
	// call; its message must carry the call's @ws_id.
	wsCancelDecorator = "ws_cancel"
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
			routeScope := service.BasePath
			if routeScope == "" {
				routeScope = src.AST.Package
			}
			if err := validateServiceDecl(src.Path, service, routeScope, seenRoutes, options); err != nil {
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
	// allowedEncodeValues is the closed set of @encode(...) wire encodings.
	allowedEncodeValues = map[string]bool{
		"number": true, "hex": true, "base64": true, "base64_raw": true,
		"base64url": true, "base64url_raw": true, "unix_seconds": true,
		"unix_millis": true, "date": true,
	}
	fieldDecorators = map[string]decoratorRule{
		"email": {}, "uuid": {}, "uri": {}, "required": {}, "nullable": {}, "unwrap": {},
		"len": {minArgs: 2, maxArgs: 2}, "range": {minArgs: 2, maxArgs: 2},
		"in": {minArgs: 1, maxArgs: -1}, "pattern": {minArgs: 1, maxArgs: 1},
		"gt": {minArgs: 1, maxArgs: 1}, "gte": {minArgs: 1, maxArgs: 1},
		"lt": {minArgs: 1, maxArgs: 1}, "lte": {minArgs: 1, maxArgs: 1},
		"min_items": {minArgs: 1, maxArgs: 1}, "max_items": {minArgs: 1, maxArgs: 1},
		flattenDecorator: {minArgs: 0, maxArgs: 1}, "encode": {minArgs: 1, maxArgs: 1},
		"empty": {minArgs: 1, maxArgs: 1}, "query": {minArgs: 0, maxArgs: 1},
		wsIDDecorator: {}, "raw": {}, "ws_timeout": {},
	}
	headerDecorators = map[string]decoratorRule{
		"required": {}, "format": {minArgs: 1, maxArgs: 1}, "example": {minArgs: 1, maxArgs: 1},
		"deprecated": {maxArgs: 1}, "auth": {minArgs: 1, maxArgs: 1}, "auth_scheme_name": {minArgs: 1, maxArgs: 1},
	}
	enumValueDecorators = map[string]decoratorRule{"json": {minArgs: 1, maxArgs: 1}}
	variantDecorators   = map[string]decoratorRule{"tag": {minArgs: 1, maxArgs: 1}, "json": {minArgs: 1, maxArgs: 1}, wsCancelDecorator: {}}
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
	if len(field.Oneof.Variants) == 0 {
		return &Error{Path: path, Line: field.Line, Msg: fmt.Sprintf("oneof %q must declare at least one variant", field.Name)}
	}
	discriminator := oneofDiscriminator(field.Oneof.Args)
	seenNames := map[string]string{}
	seenTags := map[string]string{}
	for _, variant := range field.Oneof.Variants {
		if err := validateNamedMember(path, variant.Line, variant.Name, variant.Decorators, variantDecorators, seenNames, "oneof variant"); err != nil {
			return err
		}
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
		if variant.Name == discriminator {
			return &Error{Path: path, Line: variant.Line, Msg: fmt.Sprintf(
				"oneof variant %q has the same JSON key as the discriminator; rename the variant or set oneof(discriminator: ...)", variant.Name,
			)}
		}
	}
	return nil
}

func oneofDiscriminator(args []onklang.Arg) string {
	for _, arg := range args {
		if arg.Name == oneofDiscriminatorArg {
			return arg.Value
		}
	}
	return "type"
}

func validateEnumDecl(path string, enum *onklang.EnumDecl, options CompileOptions) error {
	if err := validateDeclarationName(path, enum.Line, enum.Name); err != nil {
		return err
	}
	if len(enum.Values) == 0 {
		return &Error{Path: path, Line: enum.Line, Msg: fmt.Sprintf("enum %q must declare at least one value", enum.Name)}
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

func validateNamedMember(path string, line int, name string, decorators []onklang.Decorator, rules map[string]decoratorRule, seen map[string]string, kind string) error {
	if err := validateMemberName(path, line, name); err != nil {
		return err
	}
	if err := validateDecorators(path, line, decorators, rules); err != nil {
		return err
	}
	generated := generatedIdentifier(name)
	if previous, exists := seen[generated]; exists {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf(
			"%s %q collides with %q after target-language name conversion", kind, name, previous,
		)}
	}
	seen[generated] = name
	return nil
}

func validateOneofArgs(path string, line int, args []onklang.Arg) error {
	seen := map[string]bool{}
	for _, arg := range args {
		if arg.Name != oneofDiscriminatorArg && arg.Name != flattenDecorator {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("unknown oneof argument %q", arg.Name)}
		}
		if seen[arg.Name] {
			return &Error{Path: path, Line: line, Msg: fmt.Sprintf("duplicate oneof argument %q", arg.Name)}
		}
		seen[arg.Name] = true
		if arg.Name == oneofDiscriminatorArg && arg.Value == "" {
			return &Error{Path: path, Line: line, Msg: "oneof discriminator must not be empty"}
		}
		if arg.Name == oneofDiscriminatorArg && !isGeneratedKey(arg.Value) {
			return &Error{Path: path, Line: line, Msg: "oneof discriminator must contain only letters, digits, and underscores and must not start with a digit"}
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
	if !options.AllowLegacyContracts && hasDecorator(field.Decorators, "nullable") {
		return &Error{Path: filePath, Line: field.Line, Msg: "@nullable is unsupported; use the ? optional marker"}
	}
	if hasDecorator(field.Decorators, "query") && !isHTTPParameterTypeRef(field.Type) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@query supports string, bool, integer, and float scalar fields"}
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
		case "ws_timeout", "raw":
			if err := validateWSFieldDecorator(filePath, field, decorator.Name); err != nil {
				return err
			}
		case wsIDDecorator:
			if !isWSIDTypeRef(field.Type) || field.Repeated {
				return &Error{Path: filePath, Line: field.Line, Msg: "@ws_id requires a non-repeated string or integer field"}
			}
		case flattenDecorator, "empty":
			if field.Repeated || field.Type.IsMap || isScalarTypeRef(field.Type) {
				return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated message field", decorator.Name)}
			}
			// The two wire-mapping strategies are mutually exclusive: every
			// backend either inlines the child under a prefix or rewrites
			// its empty encoding - combining them produces conflicting
			// generated code (duplicate aux fields in Go, conflicting serde
			// attributes in Rust).
			if hasDecorator(field.Decorators, flattenDecorator) && hasDecorator(field.Decorators, "empty") {
				return &Error{Path: filePath, Line: field.Line, Msg: "@flatten cannot be combined with @empty; choose one JSON mapping for the field"}
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
		if err := validateNumericBounds(filePath, field.Line, decorator, field.Type); err != nil {
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
		pattern := value(0)
		if len(pattern) > maxCrossRuntimePatternBytes {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@pattern must not exceed %d bytes", maxCrossRuntimePatternBytes)}
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("invalid @pattern regular expression: %v", err)}
		}
		parsed, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("invalid @pattern regular expression: %v", err)}
		}
		if hasNestedRegexpRepeat(parsed, false) {
			return &Error{Path: filePath, Line: line, Msg: "@pattern contains nested repetition that is unsafe in backtracking runtimes"}
		}
	case "empty":
		if value(0) != "null" && value(0) != "omit" && value(0) != "preserve" {
			return &Error{Path: filePath, Line: line, Msg: "@empty must be null, omit, or preserve"}
		}
	case "encode":
		if !allowedEncodeValues[value(0)] {
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("unsupported @encode value %q", value(0))}
		}
	}
	return nil
}

func hasNestedRegexpRepeat(expr *syntax.Regexp, insideRepeat bool) bool {
	repeat := insideRepeat
	switch expr.Op {
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if insideRepeat {
			return true
		}
		repeat = true
	}
	for _, child := range expr.Sub {
		if hasNestedRegexpRepeat(child, repeat) {
			return true
		}
	}
	return false
}

func isScalarNamed(typ *onklang.TypeRef, name string) bool {
	return typ != nil && !typ.IsMap && typ.Name == name
}

// scalarTypeRefNames mirrors onkir.ScalarKind source spellings; hoisted to a
// package var so per-field validation does not rebuild the set per call.
var scalarTypeRefNames = map[string]bool{
	"string": true, "bool": true, "int32": true, "int64": true, "uint32": true,
	"uint64": true, "float32": true, "float64": true, "bytes": true, "timestamp": true,
	"json": true,
}

func isScalarTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	return scalarTypeRefNames[typ.Name]
}

func isHTTPParameterTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	switch typ.Name {
	case "string", "bool", "int32", "int64", "uint32", "uint64", "float32", "float64":
		return true
	default:
		return false
	}
}

func isHTTPParameterScalar(kind onkir.ScalarKind) bool {
	switch kind {
	case onkir.ScalarString, onkir.ScalarBool, onkir.ScalarInt32, onkir.ScalarInt64,
		onkir.ScalarUint32, onkir.ScalarUint64, onkir.ScalarFloat32, onkir.ScalarFloat64:
		return true
	default:
		return false
	}
}

// numericTypeRefNames lists scalar spellings usable with @gt/@gte/@lt/@lte/@range.
var numericTypeRefNames = map[string]bool{
	"int32": true, "int64": true, "uint32": true, "uint64": true, "float32": true, "float64": true,
}

func isNumericTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	return numericTypeRefNames[typ.Name]
}

// wsIDTypeRefNames lists scalar spellings usable as an @ws_id correlation
// key: hashable, discrete values only - no float/bool/bytes/timestamp/json.
var wsIDTypeRefNames = map[string]bool{
	"string": true, "int32": true, "int64": true, "uint32": true, "uint64": true,
}

func isWSIDTypeRef(typ *onklang.TypeRef) bool {
	if typ == nil || typ.IsMap {
		return false
	}
	return wsIDTypeRefNames[typ.Name]
}

func validateServiceDecl(filePath string, service *onklang.ServiceDecl, routeScope string, seenRoutes map[string]string, options CompileOptions) error {
	if err := validateDeclarationName(filePath, service.Line, service.Name); err != nil {
		return err
	}
	if err := validateHTTPPath(service.BasePath, true); err != nil {
		return &Error{Path: filePath, Line: service.Line, Msg: "invalid service base_path: " + err.Error()}
	}
	if strings.ContainsAny(service.BasePath, "{}") {
		return &Error{Path: filePath, Line: service.Line, Msg: "service base_path must not contain path parameters; declare them on each RPC route"}
	}
	if err := validateHeaders(filePath, service.Headers); err != nil {
		return err
	}
	seenMethods := map[string]string{}
	serviceHeaderNames := make(map[string]bool, len(service.Headers))
	for _, header := range service.Headers {
		serviceHeaderNames[strings.ToLower(header.Name)] = true
	}
	for _, rpc := range service.RPCs {
		if err := validateServiceRPC(filePath, rpc, service, routeScope, seenRoutes, seenMethods, serviceHeaderNames, options); err != nil {
			return err
		}
	}
	return nil
}

func validateServiceRPC(filePath string, rpc *onklang.RPCDecl, service *onklang.ServiceDecl, routeScope string, seenRoutes, seenMethods map[string]string, serviceHeaderNames map[string]bool, options CompileOptions) error {
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
	key := routeKey(verb, routeScope+route)
	if previous, exists := seenRoutes[key]; exists {
		return &Error{Path: filePath, Line: rpc.Line, Msg: fmt.Sprintf(
			"duplicate HTTP route %s (already declared by %s)", key, previous,
		)}
	}
	seenRoutes[key] = service.Name + "." + rpc.Name
	if err := validateHeaders(filePath, rpc.Headers); err != nil {
		return err
	}
	for _, header := range rpc.Headers {
		if serviceHeaderNames[strings.ToLower(header.Name)] {
			return &Error{Path: filePath, Line: header.Line, Msg: fmt.Sprintf(
				"RPC header %q conflicts with a service header", header.Name,
			)}
		}
	}
	return nil
}

func routeKey(verb, route string) string {
	if verb == wsTransport {
		verb = "GET"
	}
	return strings.ToUpper(verb) + " " + pathParameterPattern.ReplaceAllString(route, "{}")
}

func validateHeaders(path string, headers []onklang.HeaderDecl) error {
	seen := map[string]bool{}
	for _, header := range headers {
		key := strings.ToLower(header.Name)
		if key == "" {
			return &Error{Path: path, Line: header.Line, Msg: "header name must not be empty"}
		}
		if !validHTTPHeaderName(header.Name) {
			return &Error{Path: path, Line: header.Line, Msg: fmt.Sprintf("header name %q contains invalid HTTP token characters", header.Name)}
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

func validHTTPHeaderName(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return value != ""
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
		case wsDecorator:
			// Validation happens below once conflicting decorators are known;
			// record the route here.
			if len(decorator.Args) != 1 || decorator.Args[0].Value == "" {
				return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws requires one non-empty route"}
			}
			route = decorator.Args[0].Value
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
	if hasDecorator(rpc.Decorators, wsDecorator) {
		if verb != "" {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws cannot be combined with an HTTP verb; it replaces the transport binding"}
		}
		if hasDecorator(rpc.Decorators, "stream") {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws is already bidirectional and cannot be combined with @stream"}
		}
		if bodyName, ok := findDecorator(rpc.Decorators, "body"); ok {
			_ = bodyName
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@ws does not support @body binding; every non-path/non-query request field crosses as a message frame"}
		}
		if err := validateHTTPPath(route, false); err != nil {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "invalid @ws route: " + err.Error()}
		}
		return wsTransport, route, nil
	}
	if verb == "" {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "RPC must declare exactly one HTTP verb"}
	}
	if err := validateHTTPPath(route, false); err != nil {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "invalid RPC route: " + err.Error()}
	}
	if body, ok := findDecorator(rpc.Decorators, "body"); ok {
		if !isBodyBearingVerb(verb) {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires a body-bearing HTTP verb"}
		}
		if len(body.Args) != 1 || body.Args[0].Value == "" {
			return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@body requires one non-empty request field name"}
		}
	}
	if hasDecorator(rpc.Decorators, "stream") && isBodyBearingVerb(verb) {
		return "", "", &Error{Path: path, Line: rpc.Line, Msg: "@stream cannot be combined with a body-bearing HTTP verb; use @get or @delete for streaming"}
	}
	return verb, route, nil
}

// pathParameterPattern matches one `{name}` route parameter; hoisted so
// validateHTTPPath does not recompile it on every call.
var pathParameterPattern = regexp.MustCompile(`\{[^{}]+\}`)

func validateHTTPPath(value string, allowEmpty bool) error {
	if value == "" && allowEmpty {
		return nil
	}
	if !strings.HasPrefix(value, "/") {
		return errors.New("must start with /")
	}
	if strings.ContainsAny(value, "?#%") || strings.Contains(value, "//") {
		return errors.New("must not contain query strings, fragments, percent escapes, or empty segments")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return errors.New("must not contain control, quote, or backslash characters")
		}
	}
	if open := strings.Count(value, "{"); open != strings.Count(value, "}") {
		return errors.New("contains unbalanced path parameter braces")
	}
	withoutParams := pathParameterPattern.ReplaceAllString(value, "x")
	if path.Clean(withoutParams) != withoutParams {
		return fmt.Errorf("must be a canonical literal URL path (got %s after substituting path parameters)", withoutParams)
	}
	for _, segment := range strings.Split(value, "/") {
		if strings.ContainsAny(segment, "{}") && !(strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && strings.Count(segment, "{") == 1) {
			return fmt.Errorf("path parameter must fill a whole segment (got %q)", segment)
		}
	}
	for _, name := range pathParameterNames(value) {
		if strings.ContainsAny(name, "{}") {
			return fmt.Errorf("path parameter %q must not contain nested braces", name)
		}
		if name == "" || generatedIdentifier(name) != strings.ToLower(strings.ReplaceAll(name, "_", "")) {
			return fmt.Errorf("contains invalid path parameter %q", name)
		}
	}
	return nil
}

func isHTTPVerb(name string) bool {
	switch name {
	case "get", postVerb, putVerb, "delete", patchVerb, queryVerb:
		return true
	default:
		return false
	}
}

func validateDecorators(path string, line int, decorators []onklang.Decorator, rules map[string]decoratorRule) error {
	seen := map[string]bool{}
	for _, decorator := range decorators {
		line, col := line, 0
		if decorator.Line > 0 {
			line, col = decorator.Line, decorator.Col
		}
		rule, ok := rules[decorator.Name]
		if !ok {
			return &Error{Path: path, Line: line, Column: col, Msg: fmt.Sprintf("unknown decorator @%s", decorator.Name)}
		}
		if seen[decorator.Name] {
			return &Error{Path: path, Line: line, Column: col, Msg: fmt.Sprintf("duplicate decorator @%s", decorator.Name)}
		}
		seen[decorator.Name] = true
		if len(decorator.Args) < rule.minArgs || (rule.maxArgs >= 0 && len(decorator.Args) > rule.maxArgs) {
			return &Error{Path: path, Line: line, Column: col, Msg: fmt.Sprintf("@%s expects %s", decorator.Name, argCount(rule.minArgs, rule.maxArgs))}
		}
		for _, arg := range decorator.Args {
			if arg.Name != "" && (decorator.Name != flattenDecorator || arg.Name != flattenPrefixArg) {
				return &Error{Path: path, Line: line, Column: col, Msg: fmt.Sprintf("@%s does not accept named argument %q", decorator.Name, arg.Name)}
			}
			if arg.Value == "" && decorator.Name != flattenDecorator && decorator.Name != "in" {
				return &Error{Path: path, Line: line, Column: col, Msg: fmt.Sprintf("@%s arguments must not be empty", decorator.Name)}
			}
		}
		switch decorator.Name {
		case "status":
			status, err := strconv.Atoi(decorator.Args[0].Value)
			if err != nil || status < 400 || status > 599 {
				return &Error{Path: path, Line: line, Column: col, Msg: "@status must be an HTTP error status from 400 to 599"}
			}
		case "auth":
			value := decorator.Args[0].Value
			if value != "api_key" && value != "bearer" && value != "basic" {
				return &Error{Path: path, Line: line, Column: col, Msg: "@auth must be api_key, bearer, or basic"}
			}
		case flattenDecorator:
			if len(decorator.Args) == 1 && decorator.Args[0].Name != flattenPrefixArg {
				return &Error{Path: path, Line: line, Column: col, Msg: "@flatten argument must be named prefix"}
			}
			for _, arg := range decorator.Args {
				if arg.Name == flattenPrefixArg && !isGeneratedKey(arg.Value) {
					return &Error{Path: path, Line: line, Column: col, Msg: "@flatten prefix must contain only letters, digits, and underscores and must not start with a digit"}
				}
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

func isGeneratedKey(value string) bool {
	if value == "" || (value[0] >= '0' && value[0] <= '9') {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
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

// pythonMemberKeywords rejects schema member names that cannot be used as
// Python attribute names: every hard keyword (matched case-insensitively so
// "None"/"NONE" are caught too) plus the JSON literal look-alikes. Soft
// keywords (match/case) stay legal as attributes and are intentionally
// absent. Keep aligned with the module-path guard in internal/onek.
var pythonMemberKeywords = map[string]bool{
	"and": true, "as": true, "assert": true, "async": true, "await": true, "break": true,
	"class": true, "continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "false": true, "finally": true, "for": true, "from": true, "global": true,
	"if": true, "import": true, "in": true, "is": true, "lambda": true, "none": true,
	"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true, "return": true,
	"true": true, "try": true, "while": true, "with": true, "yield": true,
}

func validateDeclarationName(path string, line int, name string) error {
	if generatedIdentifier(name) == "" {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("declaration name %q does not produce a valid generated identifier", name)}
	}
	if reservedDeclarationNames[strings.ToLower(name)] {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("declaration name %q is reserved in a generated target language", name)}
	}
	return nil
}

func validateMemberName(path string, line int, name string) error {
	if generatedIdentifier(name) == "" {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("member name %q does not produce a valid generated identifier", name)}
	}
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
	if field.Type != nil && field.Type.Kind == onkir.KindMap && field.Type.MapValue != nil &&
		field.Type.MapValue.Kind == onkir.KindMessage && isRootUnwrappedMessage(field.Type.MapValue.Message) {
		return &Error{Path: filePath, Msg: fmt.Sprintf(
			"@unwrap is not supported on map value message %s; use @unwrap only on a top-level request/response message",
			field.Type.MapValue.Message.FullName(),
		)}
	}
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

func isRootUnwrappedMessage(message *onkir.Message) bool {
	return message != nil && len(message.Fields) == 1 && message.Fields[0].HasDecorator("unwrap")
}

func validateMethodBindings(filePath string, method *onkir.Method) error {
	route, ok := method.WebSocketPath()
	if !ok {
		route, _ = method.Path()
	}
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
		if field.Optional {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s cannot be optional", name, method.Name)}
		}
		if !isHTTPParameterScalar(field.Type.Scalar) {
			return &Error{Path: filePath, Msg: fmt.Sprintf("path parameter %q on RPC %s supports string, bool, integer, and float scalar fields", name, method.Name)}
		}
	}
	verb, _ := method.Verb()
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
		if isBodyBearingVerb(verb) {
			return &Error{Path: filePath, Msg: fmt.Sprintf("@query field %q is not allowed on body-bearing RPC %s", field.Name, method.Name)}
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
	if method.IsWebSocket() {
		if err := validateWSCorrelation(filePath, method); err != nil {
			return err
		}
	}
	return nil
}

func isBodyBearingVerb(verb string) bool {
	return verb == postVerb || verb == putVerb || verb == patchVerb || verb == queryVerb
}

// validateWSCorrelation enforces that @ws_id, if used at all on a @ws
// method's request/response (directly or within a oneof variant's own
// message), appears at most once per scope and shares one scalar type
// across every scope it appears in - generators emit a single generic
// pending-call map per method and need one consistent key type to key it on.
func validateWSCorrelation(filePath string, method *onkir.Method) error {
	requestFields, err := collectWSIDFields(filePath, method.Name, "request", method.Request)
	if err != nil {
		return err
	}
	responseFields, err := collectWSIDFields(filePath, method.Name, "response", method.Response)
	if err != nil {
		return err
	}
	for _, frame := range []struct {
		direction string
		message   *onkir.Message
	}{{"request", method.Request}, {"response", method.Response}} {
		if err := validateWSCancelVariants(filePath, method.Name, frame.direction, frame.message); err != nil {
			return err
		}
	}
	for _, message := range []*onkir.Message{method.Request, method.Response} {
		if err := validateWSTimeoutFields(filePath, method.Name, message); err != nil {
			return err
		}
	}
	all := append(append([]*onkir.Field{}, requestFields...), responseFields...)
	if len(all) == 0 {
		return nil
	}
	kind := all[0].Type.Scalar
	for _, field := range all[1:] {
		if field.Type == nil || field.Type.Kind != onkir.KindScalar || field.Type.Scalar != kind {
			return &Error{Path: filePath, Msg: fmt.Sprintf(
				"@ws_id fields on RPC %s must share one scalar type; found both %s and %s",
				method.Name, kind, field.Type.Scalar,
			)}
		}
	}
	return nil
}

// collectWSIDFields walks a @ws method's request or response message,
// gathering every @ws_id field found directly on it or on any oneof
// variant's own message, and rejects more than one per scope along the way.
func collectWSIDFields(filePath, methodName, direction string, message *onkir.Message) ([]*onkir.Field, error) {
	var found []*onkir.Field
	field, err := wsIDFieldInScope(filePath, methodName, direction+" message", message.Fields)
	if err != nil {
		return nil, err
	}
	if field != nil {
		found = append(found, field)
	}
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, variant := range f.Oneof.Variants {
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
				continue
			}
			variantField, err := wsIDFieldInScope(filePath, methodName, direction+" oneof variant "+variant.Name, variant.Type.Message.Fields)
			if err != nil {
				return nil, err
			}
			if variantField != nil {
				found = append(found, variantField)
			}
		}
	}
	return found, nil
}

// validateWSCancelVariants allows at most one @ws_cancel variant per frame
// message and requires its message to carry @ws_id directly: generated code
// builds the cancel frame from nothing but the abandoned call's id.
func validateWSCancelVariants(filePath, methodName, direction string, message *onkir.Message) error {
	var found *onkir.OneofVariant
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, variant := range f.Oneof.Variants {
			if !variant.IsWSCancel() {
				continue
			}
			if found != nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf(
					"%s message on RPC %s has more than one @ws_cancel variant (%s and %s)",
					direction, methodName, found.Name, variant.Name,
				)}
			}
			found = variant
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil || onkir.FindWSIDDirect(variant.Type.Message) == nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf(
					"@ws_cancel variant %s on RPC %s must be a message with a @ws_id field",
					variant.Name, methodName,
				)}
			}
		}
	}
	return nil
}

// wsIDFieldInScope returns the single @ws_id field among fields, or an error
// if more than one carries the decorator.
func wsIDFieldInScope(filePath, methodName, scope string, fields []*onkir.Field) (*onkir.Field, error) {
	var found *onkir.Field
	for _, field := range fields {
		if !field.HasDecorator(wsIDDecorator) {
			continue
		}
		if found != nil {
			return nil, &Error{Path: filePath, Msg: fmt.Sprintf("%s on RPC %s has more than one @ws_id field", scope, methodName)}
		}
		found = field
	}
	return found, nil
}

func validateWSTimeoutFields(filePath, methodName string, message *onkir.Message) error {
	check := func(scope string, m *onkir.Message) error {
		count := 0
		for _, f := range m.Fields {
			if !f.HasDecorator("ws_timeout") {
				continue
			}
			count++
			if count > 1 {
				return &Error{Path: filePath, Msg: fmt.Sprintf("%s on RPC %s has more than one @ws_timeout field", scope, methodName)}
			}
			if onkir.FindWSIDDirect(m) == nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf("@ws_timeout field %s on RPC %s must sit next to a @ws_id field", f.Name, methodName)}
			}
		}
		return nil
	}
	if err := check("message "+message.Name, message); err != nil {
		return err
	}
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, v := range f.Oneof.Variants {
			if v.Type != nil && v.Type.Kind == onkir.KindMessage && v.Type.Message != nil {
				if err := check("oneof variant "+v.Name, v.Type.Message); err != nil {
					return err
				}
			}
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

func isIntegerTypeRef(typ *onklang.TypeRef) bool {
	for _, name := range []string{"int32", "int64", "uint32", "uint64"} {
		if isScalarNamed(typ, name) {
			return true
		}
	}
	return false
}

func validateWSFieldDecorator(filePath string, field *onklang.FieldDecl, name string) error {
	if name == "ws_timeout" && (field.Repeated || field.Optional || !isIntegerTypeRef(field.Type)) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@ws_timeout requires a non-repeated, non-optional integer field"}
	}
	if name == "raw" && (field.Repeated || field.Optional || !(isScalarNamed(field.Type, "string") || isScalarNamed(field.Type, "bytes"))) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@raw requires a non-repeated, non-optional string or bytes field"}
	}
	return nil
}
