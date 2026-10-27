package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// A frontend's document may carry decorator arguments of the wrong kind;
// TypeSpec cannot write them, so these documents are built directly.
func TestDecoratorArgumentsMustBeTheRightKind(t *testing.T) {
	q, n, dec := onktest.Q, onktest.N, onktest.Dec
	cases := []struct {
		name  string
		field *onkschema.FieldDecl
		want  string
	}{
		{"quoted range", onktest.Field("n", "int32", dec("range", q("1"), q("5"))), `@range argument "1" must be a number, not a string`},
		{"quoted len", onktest.Field("s", "string", dec("len", q("1"), q("5"))), "must be a number, not a string"},
		{"quoted gte", onktest.Field("n", "int32", dec("gte", q("3"))), "must be a number, not a string"},
		{"quoted in on int", onktest.Field("n", "int32", dec("in", q("1"), q("2"))), "must be a number, not a string"},
		{"number in on string", onktest.Field("s", "string", dec("in", n("1"), n("true"))), "must be a quoted string"},
		{"number pattern", onktest.Field("s", "string", dec("pattern", n("1"))), "must be a quoted string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := onktest.File("app",
				onktest.Message("M", tc.field),
				onktest.Message("E"),
				onktest.Service("S", "", onktest.RPC("op", "M", "E", dec("post", q("/m")))),
			)
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: file}}, CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDecoratorArgumentsOfTheRightKindCompile(t *testing.T) {
	for name, field := range map[string]string{
		"strings": `@Onkt.values("a", "b") @pattern("^[a-z]+$") @minLength(1) @maxLength(5) s: string;`,
		"numbers": `@Onkt.range(1, 5) @Onkt.values(1, 2) n: int32;`,
	} {
		t.Run(name, func(t *testing.T) {
			src := "namespace app;\nmodel M {\n  " + field + "\n}\nmodel E {}\ninterface S { @post @route(\"/m\") `op`(...M): E; }\n"
			if _, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, src)}}, CompileOptions{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
