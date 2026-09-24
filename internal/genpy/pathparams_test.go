package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestPythonStreamAndSocketPathParamsFormatBooleans(t *testing.T) {
	ast, err := onklang.Parse(`
package app
message R { live: bool }
message E { v: string }
service S {
  watch(R) -> E @get("/w/{live}") @stream
  chat(R) -> E @ws("/c/{live}")
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
	if strings.Contains(out, "quote(str(req.live)") || strings.Count(out, `("true" if req.live else "false")`) != 2 {
		t.Fatalf("bool path params not formatted as true/false:\n%s", out)
	}
}
