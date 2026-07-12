package gents

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

const (
	tsTypeString  = "string"
	tsTypeNumber  = "number"
	tsTypeUnknown = "unknown"
)

func TSScalarType(k onkir.ScalarKind) string {
	switch k {
	case onkir.ScalarString, onkir.ScalarBytes, onkir.ScalarTimestamp:
		return tsTypeString
	case onkir.ScalarBool:
		return "boolean"
	case onkir.ScalarInt32, onkir.ScalarInt64, onkir.ScalarUint32, onkir.ScalarUint64,
		onkir.ScalarFloat32, onkir.ScalarFloat64:
		return tsTypeNumber
	default:
		return tsTypeUnknown
	}
}

func OneofTypeName(msg *onkir.Message, field *onkir.Field) string {
	return msg.Name + PascalCase(field.Name)
}
