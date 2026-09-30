package gendart

import (
	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

const (
	emptyBehaviorNull     = "null"
	emptyBehaviorPreserve = "preserve"
)

func fieldEncodeValue(f *onkir.Field) string {
	v, _ := genshared.FieldEncodeValue(f)
	return v
}

func int64AsString(f *onkir.Field) bool {
	return genshared.NeedsInt64StringEncoding(f)
}

func enumAsNumber(f *onkir.Field) bool {
	return genshared.NeedsEnumNumberEncoding(f)
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
