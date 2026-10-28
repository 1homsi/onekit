package genswift

import (
	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	emptyBehaviorNull = "null"
	emptyBehaviorOmit = "omit"
)

func fieldEncodeValue(f *onkir.Field) string {
	if f == nil {
		return ""
	}
	v, _ := genshared.FieldEncodeValue(f)
	return v
}

func int64AsString(f *onkir.Field) bool {
	return genshared.NeedsInt64StringEncoding(f)
}

func enumAsNumber(f *onkir.Field) bool {
	return f != nil && genshared.NeedsEnumNumberEncoding(f)
}

func bytesEncoding(f *onkir.Field) string {
	if f == nil || f.Type == nil || f.Type.Kind != onkir.KindScalar || f.Type.Scalar != onkir.ScalarBytes {
		return ""
	}
	return fieldEncodeValue(f)
}

func timestampEncoding(f *onkir.Field) string {
	if f == nil || f.Type == nil || !isTimestamp(f.Type) && !dateMap(f) {
		return ""
	}
	return fieldEncodeValue(f)
}

// dateMap reports a map of timestamps: its encoding, which the compiler only
// accepts as "date", applies to every value.
func dateMap(f *onkir.Field) bool {
	return f != nil && f.Type != nil && f.Type.Kind == onkir.KindMap && isTimestamp(f.Type.MapValue)
}

// mapValueField is the field whose encoding applies to each value of map
// field f: f itself for a map of timestamps, and nil, the plain form,
// otherwise.
func mapValueField(f *onkir.Field) *onkir.Field {
	if dateMap(f) {
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

func emptyBehavior(f *onkir.Field) string {
	return genshared.EmptyBehavior(f)
}

func rootUnwrapField(m *onkir.Message) *onkir.Field {
	return genshared.RootUnwrapField(m)
}

func oneofDiscriminator(f *onkir.Field) string {
	return genshared.OneofDiscriminator(f)
}

func fileMessagesDeep(file *onkir.File) []*onkir.Message {
	return genshared.FileMessagesDeep(file)
}

func fileEnumsDeep(file *onkir.File) []*onkir.Enum {
	out := append([]*onkir.Enum{}, file.Enums...)
	for _, m := range fileMessagesDeep(file) {
		out = append(out, m.NestedEnums...)
	}
	return out
}
