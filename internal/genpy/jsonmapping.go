package genpy

import (
	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	bytesEncodeHex          = "hex"
	bytesEncodeBase64Raw    = "base64_raw"
	bytesEncodeBase64URL    = "base64url"
	bytesEncodeBase64URLRaw = "base64url_raw"

	timestampEncodeUnixSeconds = "unix_seconds"
	timestampEncodeUnixMillis  = "unix_millis"

	emptyBehaviorNull     = "null"
	emptyBehaviorPreserve = "preserve"
)

func fieldEncodeValue(f *onkir.Field) (string, bool) {
	return genshared.FieldEncodeValue(f)
}

// needsInt64StringEncoding mirrors gengo/gents: the wire representation of an
// int64/uint64 field is a JSON string by default (cross-language JS safety),
// unless overridden with @encode(number).
func needsInt64StringEncoding(f *onkir.Field) bool {
	return genshared.NeedsInt64StringEncoding(f)
}

func needsEnumNumberEncoding(f *onkir.Field) bool {
	return genshared.NeedsEnumNumberEncoding(f)
}

func bytesEncodingValue(f *onkir.Field) string {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarBytes || f.Repeated {
		return ""
	}
	v, _ := fieldEncodeValue(f)
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
	return genshared.FlattenPrefix(f)
}

func emptyBehaviorValue(f *onkir.Field) string {
	return genshared.EmptyBehavior(f)
}

// rootUnwrapField returns the field a message should unwrap to at the root
// level: a message with exactly one field, marked @unwrap. Map-value unwrap
// is not implemented, matching gengo/gents' scope decision.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	return genshared.RootUnwrapField(m)
}

func fileNeedsBase64Import(file *onkir.File) bool {
	var containsBytes func(*onkir.Type) bool
	containsBytes = func(typ *onkir.Type) bool {
		if typ == nil {
			return false
		}
		if typ.Kind == onkir.KindScalar {
			return typ.Scalar == onkir.ScalarBytes
		}
		return typ.Kind == onkir.KindMap && containsBytes(typ.MapValue)
	}
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		for _, f := range m.Fields {
			if containsBytes(f.Type) {
				return true
			}
			if f.Oneof != nil {
				for _, v := range f.Oneof.Variants {
					if containsBytes(v.Type) {
						return true
					}
				}
			}
		}
		for _, nested := range m.Nested {
			if walk(nested) {
				return true
			}
		}
		return false
	}
	for _, m := range file.Messages {
		if walk(m) {
			return true
		}
	}
	return false
}
