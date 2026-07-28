package genrust

import (
	"strings"
	"unicode"

	"github.com/1homsi/onekit/internal/onkir"
)

const (
	rustVerbPost       = "post"
	rustVerbPut        = "put"
	rustVerbPatch      = "patch"
	rustEncodeNumber   = "number"
	validationValueVar = "value"
	decoratorEmail     = "email"
)

//nolint:gochecknoglobals // Immutable language keyword lookup shared by all naming helpers.
var rustKeywords = map[string]bool{
	"as":       true,
	"async":    true,
	"await":    true,
	"break":    true,
	"const":    true,
	"continue": true,
	"crate":    true,
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
	"self":     true,
	"Self":     true,
	"static":   true,
	"struct":   true,
	"super":    true,
	"trait":    true,
	"true":     true,
	"type":     true,
	"union":    true,
	"unsafe":   true,
	"use":      true,
	"where":    true,
	"while":    true,
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
	if rustKeywords[ident] {
		return "r#" + ident
	}
	return ident
}

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
	return out.String()
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
	default:
		return "()"
	}
}

func OneofTypeName(message *onkir.Message, field *onkir.Field) string {
	return RustMessageName(message) + PascalCase(field.Name)
}
