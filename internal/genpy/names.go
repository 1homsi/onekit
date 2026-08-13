package genpy

import (
	"strings"
	"unicode"

	"github.com/1homsi/onekit/internal/onkir"
)

func SnakeCase(s string) string {
	var sb strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func PyScalarType(k onkir.ScalarKind) string {
	switch k {
	case onkir.ScalarString, onkir.ScalarTimestamp:
		return "str"
	case onkir.ScalarBool:
		return "bool"
	case onkir.ScalarInt32, onkir.ScalarInt64, onkir.ScalarUint32, onkir.ScalarUint64:
		return "int"
	case onkir.ScalarFloat32, onkir.ScalarFloat64:
		return "float"
	case onkir.ScalarBytes:
		return "bytes"
	case onkir.ScalarJSON:
		return "Any"
	default:
		return "object"
	}
}

func fileUsesScalar(file *onkir.File, scalar onkir.ScalarKind) bool {
	var typeUsesScalar func(*onkir.Type) bool
	typeUsesScalar = func(typ *onkir.Type) bool {
		if typ == nil {
			return false
		}
		switch typ.Kind {
		case onkir.KindScalar:
			return typ.Scalar == scalar
		case onkir.KindMap:
			return typeUsesScalar(typ.MapValue)
		default:
			return false
		}
	}
	var walk func(*onkir.Message) bool
	walk = func(message *onkir.Message) bool {
		for _, field := range message.Fields {
			if typeUsesScalar(field.Type) {
				return true
			}
		}
		for _, nested := range message.Nested {
			if walk(nested) {
				return true
			}
		}
		return false
	}
	for _, message := range file.Messages {
		if walk(message) {
			return true
		}
	}
	return false
}

// PyFieldType resolves Message/Enum kinds through this printer's
// PackageResolver so a cross-module field type gets an import-qualified name
// instead of a bare (and possibly wrong) local one.
func (p *Printer) PyFieldType(t *onkir.Type) string {
	switch t.Kind {
	case onkir.KindScalar:
		return PyScalarType(t.Scalar)
	case onkir.KindMessage:
		return p.MessageTypeName(t.Message)
	case onkir.KindEnum:
		return p.EnumTypeName(t.Enum)
	case onkir.KindMap:
		return "dict[str, " + p.PyFieldType(t.MapValue) + "]"
	default:
		return "object"
	}
}

func OneofVariantClassName(msg *onkir.Message, field *onkir.Field, variant *onkir.OneofVariant) string {
	return msg.Name + PascalCase(field.Name) + PascalCase(variant.Name)
}

func PascalCase(s string) string {
	parts := strings.Split(s, "_")
	var sb strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		sb.WriteString(strings.ToUpper(part[:1]))
		sb.WriteString(part[1:])
	}
	return sb.String()
}
