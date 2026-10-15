package onkcompile

import (
	"strings"
	"testing"
)

func TestGoGeneratedNamesCannotBeDeclaredAsMessages(t *testing.T) {
	cases := []struct {
		name, schema string
		options      CompileOptions
		want         string
	}{
		{"runtime type", `message ServerOptions { a: string }
message R { a: string }
service S { op(R) -> R @post("/o") }`, CompileOptions{}, `message name "ServerOptions" collides with a type the Go generators emit`},
		{"validation errors", `message ValidationErrors { a: string }
message R { a: string }
service S { op(R) -> R @post("/o") }`, CompileOptions{}, `"ValidationErrors" collides`},
		{"service server type", `message SServer { a: string }
message R { a: string }
service S { op(R) -> R @post("/o") }`, CompileOptions{}, `collides with the type generated for service S`},
		{"service client type", `enum SClient { A }
message R { a: string }
service S { op(R) -> R @post("/o") }`, CompileOptions{}, `enum name "SClient" collides with the type generated for service S`},
		{"not generating Go", `message ServerOptions { a: string }
message R { a: string }
service S { op(R) -> R @post("/o") }`, CompileOptions{Targets: []string{"ts-client"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, "package app\n"+tc.schema+"\n")}}, tc.options)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
