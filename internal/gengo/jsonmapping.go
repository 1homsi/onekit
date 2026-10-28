package gengo

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
	timestampEncodeDate        = "date"

	emptyBehaviorNull     = "null"
	emptyBehaviorOmit     = "omit"
	emptyBehaviorPreserve = "preserve"
)

func fieldEncodeValue(f *onkir.Field) (string, bool) {
	return genshared.FieldEncodeValue(f)
}

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
	v, ok := fieldEncodeValue(f)
	if !ok || v == "base64" {
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

// timestampValueEncoding is the encoding of each timestamp f holds: the
// field's own for a single timestamp, and "date" for a list or map of
// timestamps marked @encode("date"), whose every item is YYYY-MM-DD.
func timestampValueEncoding(f *onkir.Field) string {
	if f == nil {
		return ""
	}
	if dateItems(f) {
		return timestampEncodeDate
	}
	return timestampEncodingValue(f)
}

// dateItems reports a list or map of timestamps marked @encode("date"). The
// compiler accepts no other encoding on a collection of timestamps.
func dateItems(f *onkir.Field) bool {
	if f == nil || f.Type == nil {
		return false
	}
	collection := isTimestamp(f.Type) && f.Repeated || f.Type.Kind == onkir.KindMap && isTimestamp(f.Type.MapValue)
	v, ok := fieldEncodeValue(f)
	return collection && ok && v == timestampEncodeDate
}

// mapValueField is the field whose encoding applies to each value of map
// field f: f itself for a map of dates, and nil, the plain form, otherwise.
func mapValueField(f *onkir.Field) *onkir.Field {
	if dateItems(f) {
		return f
	}
	return nil
}

func isTimestamp(t *onkir.Type) bool {
	return t != nil && t.Kind == onkir.KindScalar && t.Scalar == onkir.ScalarTimestamp
}

func flattenPrefix(f *onkir.Field) (string, bool) {
	return genshared.FlattenPrefix(f)
}

func emptyBehaviorValue(f *onkir.Field) string {
	return genshared.EmptyBehavior(f)
}

// rootUnwrapField returns the field a message should unwrap to at the root
// level: a message with exactly one field, marked @unwrap. Map-value unwrap
// (the other onk unwrap variant) is not yet implemented - it needs the
// enclosing map field's own codegen to know about this message's internal
// shape, which is a bigger structural change than root unwrap.
func rootUnwrapField(m *onkir.Message) *onkir.Field {
	return genshared.RootUnwrapField(m)
}

func fieldNeedsCustomJSON(f *onkir.Field) bool {
	if f.Oneof != nil || f.Nullable {
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
	return zeroCollectionField(f)
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
