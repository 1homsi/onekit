package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestPythonClientWarnsOnDeprecatedMethods(t *testing.T) {
	ast, err := onklang.Parse(`package app
message Note { id: string }
service Notes { get(Note) -> Note @get("/notes/{id}") @deprecated("use fetch") }
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	client := string(GenerateClient(pkg.Files[0], "models"))
	if !strings.Contains(client, `warnings.warn("get is deprecated: use fetch", DeprecationWarning, stacklevel=2)`) {
		t.Fatalf("missing deprecation warning:\n%s", client)
	}
}
