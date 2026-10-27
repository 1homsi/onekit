package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onktest"
)

func TestSingleUseRPCDecoratorsCannotRepeat(t *testing.T) {
	cases := []struct {
		name, decorators, want string
	}{
		{"stream", `@get @route("/a") @Onkt.stream @Onkt.stream`, "@stream is declared more than once"},
		{"max_body", `@post @route("/a") @Onkt.maxBody("1KiB") @Onkt.maxBody("2KiB")`, "@max_body is declared more than once"},
		{"repeatable guard", `@get @route("/a/{s}") @Onkt.guard("a/b/:s") @Onkt.guard("c/d/:s")`, ""},
		{"repeatable meta", `@get @route("/a/{s}") @Onkt.meta("k", "1") @Onkt.meta("j", "2")`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "namespace app;\nmodel A { s: string; }\ninterface S {\n  " + tc.decorators + " `op`(...A): A;\n}\n"
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, src)}}, CompileOptions{})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// A TypeSpec operation has one success status; a document with two @success
// decorators is refused.
func TestSuccessCannotRepeat(t *testing.T) {
	q, n, dec := onktest.Q, onktest.N, onktest.Dec
	file := onktest.File("app",
		onktest.Message("A", onktest.Field("s", "string")),
		onktest.Service("S", "", onktest.RPC("op", "A", "A", dec("post", q("/a")), dec("success", n("201")), dec("success", n("202")))),
	)
	_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: file}}, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "@success is declared more than once") {
		t.Fatalf("err = %v", err)
	}
}
