package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// Bounds a field cannot hold, as a document may carry them: TypeSpec already
// refuses some of these (a fractional @minLength, a string bound), and the
// compiler refuses all of them from any frontend.
func TestCompileRejectsBoundsTheFieldTypeCannotHold(t *testing.T) {
	q, n, dec, field := onktest.Q, onktest.N, onktest.Dec, onktest.Field
	tests := []struct {
		field *onkschema.FieldDecl
		want  string
	}{
		{field("s", "string", dec("len", n("1.5"), n("3"))), "non-negative integer"},
		{field("s", "string[]", dec("min_items", n("1.5"))), "non-negative integer"},
		{field("n", "int32", dec("gte", n("1.5"))), "within the range of int32"},
		{field("n", "uint32", dec("range", n("-5"), n("10"))), "within the range of uint32"},
		{field("n", "int32", dec("range", n("0"), n("99999999999"))), "within the range of int32"},
		{field("n", "float64", dec("lte", q("NaN"))), "finite decimal"},
		{field("n", "float64", dec("gt", q("0x1p4"))), "finite decimal"},
	}
	for _, tt := range tests {
		file := onktest.File("", onktest.Message("M", tt.field))
		_, err := Compile([]Source{{Path: "api.tsp", AST: file}})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%s %+v: want %q, got %v", tt.field.Name, tt.field.Decorators, tt.want, err)
		}
	}
}

func TestCompileAcceptsBoundsTheFieldTypeHolds(t *testing.T) {
	for _, field := range []string{`@Onkt.range(0.5, 1000000) n: float64;`, `@minValue(-9223372036854775808) n: int64;`} {
		if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, "model M { "+field+" }")}}); err != nil {
			t.Fatalf("%s: rejected valid bound: %v", field, err)
		}
	}
}
