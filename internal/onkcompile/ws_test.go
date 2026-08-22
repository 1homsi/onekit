package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onklang"
)

func parseSrc(src string) (*onklang.File, error) { return onklang.Parse(src) }

func TestWSMethodCompiles(t *testing.T) {
	ast, err := parseSrc(`package w

message Msg { room: string
text: string }
message Ev { seq: int64 }

service S {
  base_path: "/v1"
  chat(Msg) -> Ev @ws("/rooms/{room}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "w.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	m := pkg.Files[0].Services[0].Methods[0]
	path, _ := m.WebSocketPath()
	if !m.IsWebSocket() || path != "/rooms/{room}" {
		t.Fatalf("ws method not recognized: %+v", m)
	}
}

func TestWSRejectsConflicts(t *testing.T) {
	cases := []struct{ src, want string }{
		{`service S { f(M) -> E @get("/x") @ws("/y") }`, "@ws cannot be combined with an HTTP verb"},
		{`service S { f(M) -> E @ws("/y") @stream }`, "cannot be combined with @stream"},
		{`service S { f(M) -> E @ws("/y") @body("text") }`, "@ws does not support @body"},
		{`service S { f(M) -> E @ws() }`, "@ws requires one non-empty route"},
	}
	for _, tc := range cases {
		src := "package w\n\nmessage M { text: string }\nmessage E { x: int32 }\n\n" + tc.src
		ast, err := parseSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.want, err)
		}
		if _, err := Compile([]Source{{Path: "w.onk", AST: ast}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q got %v", tc.want, err)
		}
	}
}
