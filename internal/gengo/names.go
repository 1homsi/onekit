package gengo

import (
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

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

func CamelCase(s string) string {
	p := PascalCase(s)
	if p == "" {
		return p
	}
	return strings.ToLower(p[:1]) + p[1:]
}

func GoScalarType(k onkir.ScalarKind) string {
	switch k {
	case onkir.ScalarString:
		return "string"
	case onkir.ScalarBool:
		return "bool"
	case onkir.ScalarInt32:
		return "int32"
	case onkir.ScalarInt64:
		return "int64"
	case onkir.ScalarUint32:
		return "uint32"
	case onkir.ScalarUint64:
		return "uint64"
	case onkir.ScalarFloat32:
		return "float32"
	case onkir.ScalarFloat64:
		return "float64"
	case onkir.ScalarBytes:
		return "[]byte"
	case onkir.ScalarTimestamp:
		return "time.Time"
	case onkir.ScalarJSON:
		return "json.RawMessage"
	default:
		return "any"
	}
}

func OneofInterfaceName(msg *onkir.Message, field *onkir.Field) string {
	return msg.Name + PascalCase(field.Name)
}

func OneofVariantTypeName(msg *onkir.Message, field *onkir.Field, variant *onkir.OneofVariant) string {
	return OneofInterfaceName(msg, field) + PascalCase(variant.Name)
}
