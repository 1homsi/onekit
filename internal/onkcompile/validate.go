package onkcompile

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
		"status":    {minArgs: 1, maxArgs: 1},
		"rule":      {minArgs: 2, maxArgs: 2},
		"principal": {},
	}
	// allowedEncodeValues is the closed set of @encode(...) wire encodings.
	allowedEncodeValues = map[string]bool{
		"number": true, "hex": true, "base64": true, "base64_raw": true,
		"base64url": true, "base64url_raw": true, "unix_seconds": true,
		"unix_millis": true, "date": true,
	}
	fieldDecorators = map[string]decoratorRule{
		"email": {}, "uuid": {}, "uri": {}, "required": {}, "nullable": {}, "unwrap": {}, "object": {},
		"len": {minArgs: 2, maxArgs: 2}, "range": {minArgs: 2, maxArgs: 2},
		"in": {minArgs: 1, maxArgs: -1}, "pattern": {minArgs: 1, maxArgs: 1},
		"gt": {minArgs: 1, maxArgs: 1}, "gte": {minArgs: 1, maxArgs: 1},
		"lt": {minArgs: 1, maxArgs: 1}, "lte": {minArgs: 1, maxArgs: 1},
		"min_items": {minArgs: 1, maxArgs: 1}, "max_items": {minArgs: 1, maxArgs: 1},
		flattenDecorator: {minArgs: 0, maxArgs: 1}, "encode": {minArgs: 1, maxArgs: 1},
		"empty": {minArgs: 1, maxArgs: 1}, "query": {minArgs: 0, maxArgs: 1},
		wsIDDecorator: {}, "raw": {}, "ws_timeout": {}, "deprecated": {maxArgs: 1},
		"rule": {minArgs: 2, maxArgs: 2},
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
	for _, decorator := range field.Decorators {
		if decorator.Name != "required" {
			return &Error{Path: path, Line: field.Line, Msg: fmt.Sprintf("@%s is not supported on oneof field %q; only @required is", decorator.Name, field.Name)}
		}
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

func validateFieldDecoratorType(filePath string, field *onklang.FieldDecl, name string) error {
	switch name {
	case "in":
		if !acceptsIn(field) {
			return &Error{Path: filePath, Line: field.Line, Msg: "@in requires a non-repeated string or integer field"}
		}
	case "email", "uuid", "uri", "pattern", "len":
		if !isScalarNamed(field.Type, "string") || field.Repeated {
			return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated string field", name)}
		}
	case "gt", "gte", "lt", "lte", "range":
		if !isNumericTypeRef(field.Type) || field.Repeated {
			return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated numeric field", name)}
		}
	case "object":
		if !isScalarNamed(field.Type, "json") || field.Repeated {
			return &Error{Path: filePath, Line: field.Line, Msg: "@object requires a non-repeated json field"}
		}
	case "min_items", "max_items":
		if !field.Repeated {
			return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a repeated field", name)}
		}
	case "ws_timeout", "raw":
		if err := validateWSFieldDecorator(filePath, field, name); err != nil {
			return err
		}
	case wsIDDecorator:
		if !isWSIDTypeRef(field.Type) || field.Repeated {
			return &Error{Path: filePath, Line: field.Line, Msg: "@ws_id requires a non-repeated string or integer field"}
		}
	case flattenDecorator, "empty":
		if field.Repeated || field.Type.IsMap || isScalarTypeRef(field.Type) {
			return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("@%s requires a non-repeated message field", name)}
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
	return nil
}

func acceptsIn(field *onklang.FieldDecl) bool {
	if field.Type.IsMap || field.Repeated {
		return false
	}
	_, integer := integerBounds[field.Type.Name]
	return integer || isScalarNamed(field.Type, "string")
}

func validateFieldDecoratorSemantics(filePath string, field *onklang.FieldDecl, options CompileOptions) error {
	if field.Type == nil {
		return nil
	}
	if field.Optional && field.Type.IsMap {
		return &Error{Path: filePath, Line: field.Line, Msg: fmt.Sprintf("map field %q cannot be optional; an empty map already means no entries", field.Name)}
	}
	if hasDecorator(field.Decorators, "query") && !isHTTPParameterTypeRef(field.Type) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@query supports string, bool, integer, and float scalar fields"}
	}
	for _, decorator := range field.Decorators {
		if err := validateFieldDecoratorType(filePath, field, decorator.Name); err != nil {
			return err
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
		if seen[decorator.Name] && decorator.Name != "rule" {
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
	if first, _ := utf8.DecodeRuneInString(name); !unicode.IsUpper(first) {
		return &Error{Path: path, Line: line, Msg: fmt.Sprintf("declaration name %q must start with an uppercase letter so generated Go code exports it", name)}
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
