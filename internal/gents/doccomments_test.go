package gents

import (
	"strings"
	"testing"
)

func TestTSOutputCarriesJSDoc(t *testing.T) {
	file := compileTSSchema(t, `package app
/// A stored note.
///
/// Notes end with */ safely.
message Note {
  /// Unique identifier.
  id: string
}
/// Visibility of a note.
enum Visibility { PRIVATE PUBLIC }
/// Manages notes.
service Notes {
  /// Fetch one note.
  get(Note) -> Note @get("/notes/{id}")
}
`)
	types, server, client := string(GenerateTypes(file)), string(GenerateServer(file)), string(GenerateClient(file))
	for _, check := range []struct{ out, want string }{
		{types, "/**\n * A stored note.\n *\n * Notes end with *\\/ safely.\n */\nexport interface Note {"},
		{types, "/** Unique identifier. */\nid"},
		{types, "/** Visibility of a note. */\nexport type Visibility"},
		{server, "/** Manages notes. */\nexport interface NotesHandler {\n/** Fetch one note. */\nget(req"},
		{client, "/** Manages notes. */\nexport class NotesClient {"},
		{client, "/** Fetch one note. */\nasync get(req"},
	} {
		if !strings.Contains(check.out, check.want) {
			t.Fatalf("missing %q in:\n%s", check.want, check.out)
		}
	}
}
