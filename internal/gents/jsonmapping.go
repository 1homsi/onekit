package gents

import (
	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	timestampEncodeUnixSeconds = "unix_seconds"
	timestampEncodeUnixMillis  = "unix_millis"
	timestampEncodeDate        = "date"

	emptyBehaviorNull     = "null"
	emptyBehaviorOmit     = "omit"
	emptyBehaviorPreserve = "preserve"
)

func fieldEncodeValue(f *onkir.Field) (string, bool) {
	return genshared.FieldEncodeValue(f)
}

// needsInt64NumberEncoding reports whether an int64/uint64 field should use
// the TS "number" type on the wire instead of the JS-safe default "string".
func needsInt64NumberEncoding(f *onkir.Field) bool {
	return genshared.NeedsInt64NumberEncoding(f)
}

// needsEnumNumberEncoding mirrors gengo: only non-repeated enum fields honor
// @encode(number) - the enum's own default string representation is defined
// once at the type level, so repeated fields can't override it per-field.
func needsEnumNumberEncoding(f *onkir.Field) bool {
	return genshared.NeedsEnumNumberEncoding(f)
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
	return genshared.FlattenPrefix(f)
}

func emptyBehaviorValue(f *onkir.Field) string {
	return genshared.EmptyBehavior(f)
}

// rootUnwrapField returns the field a message should unwrap to at the root
// level: a message with exactly one field, marked @unwrap. Map-value unwrap
// is not implemented, matching gengo's scope decision.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	return genshared.RootUnwrapField(m)
}
