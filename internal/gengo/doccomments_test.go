package gengo

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestGoOutputCarriesDocComments(t *testing.T) {
	ast, err := onklang.Parse(`package app
/// A stored note.
///
/// Notes are immutable.
message Note {
  /// Unique identifier.
  id: string
}
/// Visibility of a note.
enum Visibility {
  /// Only the author.
  PRIVATE
  PUBLIC
}
/// Manages notes.
service Notes {
  /// Fetch one note.
  get(Note) -> Note @get("/notes/{id}")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	types, _ := GenerateTypes(file)
	server, _ := GenerateServer(file)
	client, _ := GenerateClient(file)
	for name, check := range map[string]struct {
		out  []byte
		want []string
	}{
		"types":  {types, []string{"// A stored note.\n//\n// Notes are immutable.\ntype Note struct {", "\t// Unique identifier.\n\tId string", "// Visibility of a note.\ntype Visibility int32", "\t// Only the author.\n\tVisibilityPrivate Visibility = iota"}},
		"server": {server, []string{"// Manages notes.\ntype NotesServer interface {", "\t// Fetch one note.\n\tGet(ctx"}},
		"client": {client, []string{"// Fetch one note.\nfunc (c *NotesClient) Get("}},
	} {
		for _, want := range check.want {
			if !strings.Contains(string(check.out), want) {
				t.Fatalf("%s is missing %q:\n%s", name, want, check.out)
			}
		}
	}
}
