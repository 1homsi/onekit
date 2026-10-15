package onkcompile

import (
	"strings"
	"testing"
)

func TestSingleUseRPCDecoratorsCannotRepeat(t *testing.T) {
	cases := []struct {
		name, rpc, want string
	}{
		{"stream", `op(A) -> A @get("/a") @stream @stream`, "@stream is declared more than once"},
		{"success", `op(A) -> A @post("/a") @success(201) @success(202)`, "@success is declared more than once"},
		{"max_body", `op(A) -> A @post("/a") @max_body("1KiB") @max_body("2KiB")`, "@max_body is declared more than once"},
		{"repeatable guard", `op(A) -> A @get("/a/{s}") @guard("a/b/:s") @guard("c/d/:s")`, ""},
		{"repeatable meta", `op(A) -> A @get("/a/{s}") @meta("k", "1") @meta("j", "2")`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package app\nmessage A { s: string }\nservice S {\n  " + tc.rpc + "\n}\n"
			_, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}}, CompileOptions{})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
