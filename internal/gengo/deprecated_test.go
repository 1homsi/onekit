package gengo

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const deprecatedFixture = `package app
message Note {
  id: string
  /// Legacy title.
  title: string @deprecated("use heading")
  body: string @deprecated
}
service Notes {
  /// Fetch one note.
  get(Note) -> Note @get("/notes/{id}") @deprecated("use fetch")
  fetch(Note) -> Note @get("/v2/notes/{id}")
}
`

func TestGoMarksDeprecatedFieldsAndMethods(t *testing.T) {
	ast, err := onklang.Parse(deprecatedFixture)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	types, _ := GenerateTypes(pkg.Files[0])
	server, _ := GenerateServer(pkg.Files[0])
	client, _ := GenerateClient(pkg.Files[0])
	for _, check := range []struct{ out, want string }{
		{string(types), "\t// Legacy title.\n\t//\n\t// Deprecated: use heading\n\tTitle string"},
		{string(types), "\t// Deprecated: no longer supported.\n\tBody string"},
		{string(server), "\t// Fetch one note.\n\t//\n\t// Deprecated: use fetch\n\tGet(ctx"},
		{string(client), "// Deprecated: use fetch\nfunc (c *NotesClient) Get("},
	} {
		if !strings.Contains(check.out, check.want) {
			t.Fatalf("missing %q in:\n%s", check.want, check.out)
		}
	}
}
