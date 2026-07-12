package gents

import (
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	encodeNumber = "number"

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

// needsInt64NumberEncoding reports whether an int64/uint64 field should use
// the TS "number" type on the wire instead of the JS-safe default "string".
func needsInt64NumberEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || !isInt64Kind(f.Type.Scalar) {
		return false
	}
	v, _ := fieldEncodeValue(f)
	return v == encodeNumber
}

// needsEnumNumberEncoding mirrors gengo: only non-repeated enum fields honor
// @encode(number) - the enum's own default string representation is defined
// once at the type level, so repeated fields can't override it per-field.
func needsEnumNumberEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindEnum || f.Repeated {
		return false
	}
	v, _ := fieldEncodeValue(f)
	return v == encodeNumber
}

// timestampEncodingValue mirrors gengo: only non-repeated timestamp fields
// can override the default RFC3339-string wire representation.
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
// is not implemented, matching gengo's scope decision.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	if len(m.Fields) == 1 && isUnwrapField(m.Fields[0]) {
		return m.Fields[0]
	}
	return nil
}

// messageEmptyFields returns the message-typed fields on m with a non-default
// @empty behavior - the only annotation that needs runtime encode support in
// TypeScript, since @flatten and @unwrap are resolved entirely at the type
// level (the generated TS type already mirrors the wire shape).
func messageEmptyFields(m *onkir.Message) []*onkir.Field {
	var fields []*onkir.Field
	for _, f := range m.Fields {
		if v := emptyBehaviorValue(f); v != "" && v != emptyBehaviorPreserve {
			fields = append(fields, f)
		}
	}
	return fields
}

func messageNeedsEncodeHelper(m *onkir.Message) bool {
	return len(messageEmptyFields(m)) > 0
}
