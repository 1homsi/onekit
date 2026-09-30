package gendart

import "github.com/1homsi/onekit/internal/onkir"

const (
	encodeNumber = "number"

	emptyBehaviorNull     = "null"
	emptyBehaviorPreserve = "preserve"
)

func fieldEncodeValue(f *onkir.Field) string {
	d, ok := f.Decorator("encode")
	if !ok {
		return ""
	}
	v, _ := d.Value()
	return v
}

func isInt64Kind(k onkir.ScalarKind) bool {
	return k == onkir.ScalarInt64 || k == onkir.ScalarUint64
}

func int64AsString(f *onkir.Field) bool {
	return f != nil && f.Type != nil && f.Type.Kind == onkir.KindScalar && isInt64Kind(f.Type.Scalar) && fieldEncodeValue(f) != encodeNumber
}

func enumAsNumber(f *onkir.Field) bool {
	return f != nil && f.Type != nil && f.Type.Kind == onkir.KindEnum && fieldEncodeValue(f) == encodeNumber
}

func bytesEncoding(f *onkir.Field) string {
	if f == nil || f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarBytes {
		return ""
	}
	return fieldEncodeValue(f)
}

func timestampEncoding(f *onkir.Field) string {
	if f == nil || f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarTimestamp {
		return ""
	}
	return fieldEncodeValue(f)
}

func flattenPrefix(f *onkir.Field) (string, bool) {
	if f.Type == nil || f.Type.Kind != onkir.KindMessage || f.Repeated {
		return "", false
	}
	d, ok := f.Decorator("flatten")
	if !ok {
		return "", false
	}
	prefix, _ := d.NamedArg("prefix")
	return prefix, true
}

func emptyBehavior(f *onkir.Field) string {
	if f.Type == nil || f.Type.Kind != onkir.KindMessage || f.Repeated {
		return ""
	}
	d, ok := f.Decorator("empty")
	if !ok {
		return ""
	}
	v, _ := d.Value()
	return v
}

func rootUnwrapField(m *onkir.Message) *onkir.Field {
	if len(m.Fields) == 1 && m.Fields[0].HasDecorator("unwrap") {
		return m.Fields[0]
	}
	return nil
}

func oneofDiscriminator(f *onkir.Field) string {
	if disc, ok := f.Oneof.Discriminator(); ok && disc != "" {
		return disc
	}
	return "type"
}

func fileMessagesDeep(file *onkir.File) []*onkir.Message {
	var out []*onkir.Message
	var walk func([]*onkir.Message)
	walk = func(ms []*onkir.Message) {
		for _, m := range ms {
			out = append(out, m)
			walk(m.Nested)
		}
	}
	walk(file.Messages)
	return out
}

func fileEnumsDeep(file *onkir.File) []*onkir.Enum {
	out := append([]*onkir.Enum{}, file.Enums...)
	for _, m := range fileMessagesDeep(file) {
		out = append(out, m.NestedEnums...)
	}
	return out
}
