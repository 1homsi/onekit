package genpy

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

	emptyBehaviorNull     = "null"
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

// needsInt64StringEncoding mirrors gengo/gents: the wire representation of an
// int64/uint64 field is a JSON string by default (cross-language JS safety),
// unless overridden with @encode(number). Optional fields are excluded, same
// documented gap as the other generators.
func needsInt64StringEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || !isInt64Kind(f.Type.Scalar) || f.Optional {
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
// is not implemented, matching gengo/gents' scope decision.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	if len(m.Fields) == 1 && isUnwrapField(m.Fields[0]) {
		return m.Fields[0]
	}
	return nil
}

func fileNeedsBase64Import(file *onkir.File) bool {
	var walk func(m *onkir.Message) bool
	walk = func(m *onkir.Message) bool {
		for _, f := range m.Fields {
			if f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBytes && !f.Repeated {
				return true
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
