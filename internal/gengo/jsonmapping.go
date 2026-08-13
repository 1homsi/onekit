package gengo

import (
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	encodeNumber = "number"

	bytesEncodeHex          = "hex"
	bytesEncodeBase64Raw    = "base64_raw"
	bytesEncodeBase64URL    = "base64url"
	bytesEncodeBase64URLRaw = "base64url_raw"

	timestampEncodeUnixSeconds = "unix_seconds"
	timestampEncodeUnixMillis  = "unix_millis"
	timestampEncodeDate        = "date"

	emptyBehaviorNull     = "null"
	emptyBehaviorOmit     = "omit"
	emptyBehaviorPreserve = "preserve"
)

func isInt64Kind(k onkir.ScalarKind) bool {
	return k == onkir.ScalarInt64 || k == onkir.ScalarUint64
}

func fieldEncodeValue(f *onkir.Field) (string, bool) {
	d, ok := f.Decorator("encode")
	if !ok {
		return "", false
	}
	return d.Value()
}

func needsInt64StringEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || !isInt64Kind(f.Type.Scalar) {
		return false
	}
	v, _ := fieldEncodeValue(f)
	return v != encodeNumber
}

func needsEnumNumberEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindEnum || f.Repeated {
		return false
	}
	v, _ := fieldEncodeValue(f)
	return v == encodeNumber
}

func bytesEncodingValue(f *onkir.Field) string {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarBytes || f.Repeated {
		return ""
	}
	v, ok := fieldEncodeValue(f)
	if !ok {
		return ""
	}
	return v
}

func timestampEncodingValue(f *onkir.Field) string {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarTimestamp || f.Repeated {
		return ""
	}
	v, ok := fieldEncodeValue(f)
	if !ok {
		return ""
	}
	return v
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

func emptyBehaviorValue(f *onkir.Field) string {
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

func isUnwrapField(f *onkir.Field) bool {
	return f.HasDecorator("unwrap")
}

// rootUnwrapField returns the field a message should unwrap to at the root
// level: a message with exactly one field, marked @unwrap. Map-value unwrap
// (the other onk unwrap variant) is not yet implemented - it needs the
// enclosing map field's own codegen to know about this message's internal
// shape, which is a bigger structural change than root unwrap.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	if len(m.Fields) == 1 && isUnwrapField(m.Fields[0]) {
		return m.Fields[0]
	}
	return nil
}

func fieldNeedsCustomJSON(f *onkir.Field) bool {
	if f.Oneof != nil {
		return true
	}
	if needsInt64StringEncoding(f) {
		return true
	}
	if needsEnumNumberEncoding(f) {
		return true
	}
	if bytesEncodingValue(f) != "" {
		return true
	}
	if timestampEncodingValue(f) != "" {
		return true
	}
	if _, ok := flattenPrefix(f); ok {
		return true
	}
	if v := emptyBehaviorValue(f); v != "" && v != emptyBehaviorPreserve {
		return true
	}
	return false
}

func messageNeedsCustomJSON(m *onkir.Message) bool {
	if rootUnwrapField(m) != nil {
		return false // handled by writeRootUnwrapJSON instead
	}
	for _, f := range m.Fields {
		if fieldNeedsCustomJSON(f) {
			return true
		}
	}
	return false
}

func messageOrNestedNeedsCustomJSON(m *onkir.Message) bool {
	if messageNeedsCustomJSON(m) || rootUnwrapField(m) != nil {
		return true
	}
	for _, nested := range m.Nested {
		if messageOrNestedNeedsCustomJSON(nested) {
			return true
		}
	}
	return false
}

func fileNeedsJSONHelpers(file *onkir.File) bool {
	if hasOneofMessages(file) {
		return true
	}
	for _, m := range file.Messages {
		if messageOrNestedNeedsCustomJSON(m) {
			return true
		}
	}
	return false
}

type encodingImports struct {
	hex     bool
	base64  bool
	strconv bool
}

func fileNeedsEncodingImports(file *onkir.File) encodingImports {
	var imp encodingImports
	var walk func(m *onkir.Message)
	walk = func(m *onkir.Message) {
		for _, f := range m.Fields {
			if needsInt64StringEncoding(f) {
				imp.strconv = true
			}
			switch bytesEncodingValue(f) {
			case bytesEncodeHex:
				imp.hex = true
			case bytesEncodeBase64Raw, bytesEncodeBase64URL, bytesEncodeBase64URLRaw:
				imp.base64 = true
			}
		}
		for _, nested := range m.Nested {
			walk(nested)
		}
	}
	for _, m := range file.Messages {
		walk(m)
	}
	return imp
}
