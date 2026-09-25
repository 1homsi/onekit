package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestPythonClientKeepsBuiltinsWhenErrorsShadowThem(t *testing.T) {
	ast, err := onklang.Parse(`
package app
message Msg { text: string }
message TimeoutError @status(504) { message: string }
message ConnectionError @status(502) { message: string }
service Chat {
  send(Msg) -> Msg | TimeoutError | ConnectionError @post("/send")
  stream(Msg) -> Msg @ws("/chat")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	out := string(GenerateClient(pkg.Files[0], "models"))
	for _, bad := range []string{"(ConnectionError):", "(TimeoutError):", "except TimeoutError"} {
		if strings.Contains(out, bad) {
			t.Fatalf("runtime refers to a name the schema shadows: %q", bad)
		}
	}
	if !strings.Contains(out, "import builtins as _builtins") {
		t.Fatal("builtins import missing")
	}
}
