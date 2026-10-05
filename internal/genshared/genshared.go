// Package genshared holds the schema-query helpers every target-language
// generator (gengo, gents, genpy, genrust, gendart, genopenapi) needs to
// answer the same questions about a compiled onkir schema: is this field
// flattened, does it fall back to root @unwrap, does it cross the wire as a
// string or a number, does this oneof have a custom discriminator. Each
// generator previously carried its own copy of these; a schema-wire rule
// changing in one copy and not another is how the cross-target int64/oneof
// and @flatten mismatches happened. This package is the single place those
// rules live now.
package genshared

import "github.com/1homsi/onekit/internal/onkir"

const encodeNumber = "number"

// FieldEncodeValue returns a field's @encode argument, if any.
func FieldEncodeValue(f *onkir.Field) (string, bool) {
	d, ok := f.Decorator("encode")
	if !ok {
		return "", false
	}
	return d.Value()
}

// IsInt64Kind reports whether k is int64 or uint64.
func IsInt64Kind(k onkir.ScalarKind) bool {
	return k == onkir.ScalarInt64 || k == onkir.ScalarUint64
}

// NeedsInt64StringEncoding reports whether an int64/uint64 field's wire
// representation is a JSON string (the default, for cross-language safety)
// rather than a bare number (@encode("number")).
func NeedsInt64StringEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || !IsInt64Kind(f.Type.Scalar) {
		return false
	}
	return !int64IsNumber(f)
}

func int64IsNumber(f *onkir.Field) bool {
	if f.Int64Number {
		return true
	}
	v, _ := FieldEncodeValue(f)
	return v == encodeNumber
}

// NeedsInt64NumberEncoding is NeedsInt64StringEncoding's complement: it
// reports whether an int64/uint64 field is sent as a bare number
// (@encode("number")) rather than the default JSON string.
func NeedsInt64NumberEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindScalar || !IsInt64Kind(f.Type.Scalar) {
		return false
	}
	return int64IsNumber(f)
}

// NeedsEnumNumberEncoding reports whether an enum field is sent as its
// numeric ordinal (@encode("number")) rather than its JSON name.
func NeedsEnumNumberEncoding(f *onkir.Field) bool {
	if f.Type == nil || f.Type.Kind != onkir.KindEnum || f.Repeated {
		return false
	}
	v, _ := FieldEncodeValue(f)
	return v == encodeNumber
}

// FlattenPrefix returns a message field's @flatten(prefix: ...) argument.
// The compiler only allows @flatten on a non-repeated message field, so the
// guard here only ever matters for a still-unvalidated AST.
func FlattenPrefix(f *onkir.Field) (string, bool) {
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

// EmptyBehavior returns a message field's @empty(...) argument.
func EmptyBehavior(f *onkir.Field) string {
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

// RootUnwrapField returns the field a message should unwrap to at the root
// level: a message with exactly one field, marked @unwrap.
func RootUnwrapField(m *onkir.Message) *onkir.Field {
	if len(m.Fields) == 1 && m.Fields[0].HasDecorator("unwrap") {
		return m.Fields[0]
	}
	return nil
}

// OneofDiscriminator returns a oneof's discriminator key, defaulting to
// "type" when none is declared.
func OneofDiscriminator(f *onkir.Field) string {
	if disc, ok := f.Oneof.Discriminator(); ok && disc != "" {
		return disc
	}
	return "type"
}

// FileMessagesDeep flattens a file's message tree (including nested
// messages) into a single slice, in declaration order.
func FileMessagesDeep(file *onkir.File) []*onkir.Message {
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
