package genrust

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileRustSchema(t *testing.T, schema string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func TestRustServerErrorBodiesUseMessageKey(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `
package app
message R { id: string @len(1, 10) }
service S { get(R) -> R @get("/r/{id}") }
`)))
	if strings.Contains(out, `json!({ "error":`) || !strings.Contains(out, `json!({ "message": "internal server error" })`) {
		t.Fatalf("error bodies must use the message key like Go and TypeScript:\n%s", out)
	}
}
