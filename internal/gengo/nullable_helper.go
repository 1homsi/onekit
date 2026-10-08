package gengo

import "github.com/1homsi/onekit/internal/onkir"

func fileHasShadowNullable(file *onkir.File) bool {
	for _, m := range fileMessagesDeep(file) {
		if rootUnwrapField(m) != nil {
			continue
		}
		c := categorizeFields(m)
		if len(c.nulls) > 0 && len(c.flattens) == 0 {
			return true
		}
	}
	return false
}

func writeNullableHelper(p *Printer) {
	p.P("type onkNullable[T any] struct {")
	p.P("V    *T")
	p.P("Null bool")
	p.P("}")
	p.P()
	p.P("func (n onkNullable[T]) IsZero() bool { return n.V == nil && !n.Null }")
	p.P()
	p.P("func (n onkNullable[T]) MarshalJSON() ([]byte, error) {")
	p.P("if n.V == nil {")
	p.P(`return []byte("null"), nil`)
	p.P("}")
	p.P("return json.Marshal(n.V)")
	p.P("}")
	p.P()
	p.P("func (n onkNullable[T]) MarshalJSONTo(enc *jsontext.Encoder) error {")
	p.P("if n.V == nil {")
	p.P("return enc.WriteToken(jsontext.Null)")
	p.P("}")
	p.P("return jsonv2.MarshalEncode(enc, n.V)")
	p.P("}")
	p.P()
	p.P("func (n *onkNullable[T]) UnmarshalJSON(data []byte) error {")
	p.P(`if string(data) == "null" {`)
	p.P("n.V, n.Null = nil, true")
	p.P("return nil")
	p.P("}")
	p.P("n.Null = false")
	p.P("n.V = new(T)")
	p.P("return json.Unmarshal(data, n.V)")
	p.P("}")
	p.P()
}
