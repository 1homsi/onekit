package genrust

import (
	"strings"
	"unicode"

	"github.com/1homsi/onekit/internal/onkir"
)

const (
	rustVerbPost       = "post"
	queryVerb          = "query"
	rustVerbPut        = "put"
	rustVerbPatch      = "patch"
	rustEncodeNumber   = "number"
	validationValueVar = "value"
	decoratorEmail     = "email"
	decoratorGt        = "gt"
	decoratorGte       = "gte"
	decoratorLt        = "lt"
	decoratorLte       = "lte"
	// fallbackSerdeOwner names serde_with helper modules for fields whose
	// message back link is missing, mirroring the zero-value guard in the
	// module name helpers.
	fallbackSerdeOwner = "message"
)

// rustNonRawKeywords lists the four keywords Rust refuses to spell in raw
// identifier form. `r#self`, `r#Self`, `r#super` and `r#crate` are all hard
// parse errors ("`self` cannot be a raw identifier"), so they are the only
// keywords that cannot be escaped with a `r#` prefix.
//
//nolint:gochecknoglobals // Immutable language keyword lookup shared by all naming helpers.
var rustNonRawKeywords = map[string]bool{
	"self":  true,
	"Self":  true,
	"super": true,
	"crate": true,
}

// nonRawIdentifierSuffix is appended to the rustNonRawKeywords names. Suffixing
// (rather than any other rewrite) is safe because every generated field and
// enum variant already carries a serde rename pinning the original wire name,
// so the Rust-side spelling is purely cosmetic.
const nonRawIdentifierSuffix = "_"

// rustKeywords holds the Rust keywords that are legal in raw identifier form
// (r#fn, r#type, ...) and therefore need no rewriting beyond the r# prefix.
// rustNonRawKeywords is deliberately NOT a subset of this set: those four
// names cannot be spelled with r# and must be mangled by suffix instead.
//
//nolint:gochecknoglobals // Immutable language keyword lookup shared by all naming helpers.
var rustKeywords = map[string]bool{
	"as":       true,
	"async":    true,
	"await":    true,
	"break":    true,
	"const":    true,
	"continue": true,
	"dyn":      true,
	"else":     true,
	"enum":     true,
	"extern":   true,
	"false":    true,
	"fn":       true,
	"for":      true,
	"if":       true,
	"impl":     true,
	"in":       true,
	"let":      true,
	"loop":     true,
	"match":    true,
	"mod":      true,
	"move":     true,
	"mut":      true,
	"pub":      true,
	"ref":      true,
	"return":   true,
	"static":   true,
	"struct":   true,
	"trait":    true,
	"true":     true,
	"type":     true,
	"union":    true,
	"unsafe":   true,
	"use":      true,
	"where":    true,
	"while":    true,
	"abstract": true,
	"become":   true,
	"box":      true,
	"do":       true,
	"final":    true,
	"gen":      true,
	"macro":    true,
	"override": true,
	"priv":     true,
	"try":      true,
	"typeof":   true,
	"unsized":  true,
	"virtual":  true,
	"yield":    true,
}

// mangleNonRawKeyword makes a generated identifier legal Rust. The four
// keywords that cannot use the r# escape form get a trailing underscore
// instead; every other keyword keeps the r# prefix.
//
// A single pass is enough and there is no recursion: appending the suffix
// always lands outside rustNonRawKeywords, so mangling is idempotent and
// cannot loop.
func mangleNonRawKeyword(ident string) string {
	if rustNonRawKeywords[ident] {
		return ident + nonRawIdentifierSuffix
	}
	if rustKeywords[ident] {
		return "r#" + ident
	}
	return ident
}

func SnakeCase(value string) string {
	var out strings.Builder
	var previousLower bool
	for i, r := range value {
		if r == '-' || r == ' ' {
			if out.Len() > 0 && !strings.HasSuffix(out.String(), "_") {
				out.WriteByte('_')
			}
			previousLower = false
			continue
		}
		if unicode.IsUpper(r) {
			if i > 0 && previousLower {
				out.WriteByte('_')
			}
			out.WriteRune(unicode.ToLower(r))
			previousLower = false
			continue
		}
		out.WriteRune(unicode.ToLower(r))
		previousLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return out.String()
}

func RustIdent(value string) string {
	ident := SnakeCase(value)
	if ident == "" {
		return "_"
	}
	if ident[0] >= '0' && ident[0] <= '9' {
		ident = "_" + ident
	}
	return mangleNonRawKeyword(ident)
}

// ErrorVariantNames maps each of a method's error types to its Rust enum
// variant name, positionally. The "Error" suffix is trimmed for readability
// (NotFoundError -> NotFound), but when trimming would make two error types
// collide (e.g. sibling declarations Foo and FooError), the full PascalCase
// name is kept for every member of the colliding group so generated enums
// always compile.
var builtinErrorVariants = map[string]bool{
	"Validation": true, "InvalidRequest": true, "Transport": true, "WsTransport": true, "Response": true,
	"Decode": true, "Stream": true, "Internal": true, "UnexpectedStatus": true,
}

func ErrorVariantNames(errorTypes []*onkir.Message) []string {
	names := make([]string, len(errorTypes))
	counts := map[string]int{}
	for i, message := range errorTypes {
		name := PascalCase(strings.TrimSuffix(message.Name, "Error"))
		if name == "" || builtinErrorVariants[name] {
			name = PascalCase(message.Name)
		}
		if builtinErrorVariants[name] {
			name += "Error"
		}
		names[i] = name
		counts[name]++
	}
	for i, name := range names {
		if counts[name] > 1 {
			names[i] = PascalCase(errorTypes[i].Name)
		}
	}
	return names
}

// PascalCase converts a schema name to the Rust type / enum variant spelling.
// The result is passed through mangleNonRawKeyword because `self` PascalCases to
// `Self`, one of the four keywords that cannot be raw-escaped.
func PascalCase(value string) string {
	var out strings.Builder
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.'
	})
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == strings.ToUpper(part) {
			part = strings.ToLower(part)
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		for _, r := range runes {
			out.WriteRune(r)
		}
	}
	return mangleNonRawKeyword(out.String())
}

func RustMessageName(message *onkir.Message) string {
	var names []string
	for current := message; current != nil; current = current.Parent {
		names = append([]string{PascalCase(current.Name)}, names...)
	}
	return strings.Join(names, "")
}

func RustEnumName(enum *onkir.Enum) string {
	var names []string
	for current := enum.Parent; current != nil; current = current.Parent {
		names = append([]string{PascalCase(current.Name)}, names...)
	}
	names = append(names, PascalCase(enum.Name))
	return strings.Join(names, "")
}

func RustScalarType(kind onkir.ScalarKind) string {
	switch kind {
	case onkir.ScalarString, onkir.ScalarTimestamp:
		return "String"
	case onkir.ScalarBool:
		return "bool"
	case onkir.ScalarInt32:
		return "i32"
	case onkir.ScalarInt64:
		return "i64"
	case onkir.ScalarUint32:
		return "u32"
	case onkir.ScalarUint64:
		return "u64"
	case onkir.ScalarFloat32:
		return "f32"
	case onkir.ScalarFloat64:
		return "f64"
	case onkir.ScalarBytes:
		return "Vec<u8>"
	case onkir.ScalarJSON:
		return "serde_json::Value"
	default:
		return "()"
	}
}

func OneofTypeName(message *onkir.Message, field *onkir.Field) string {
	return RustMessageName(message) + PascalCase(field.Name)
}
