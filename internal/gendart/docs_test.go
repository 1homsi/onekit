package gendart

import (
	"strings"
	"testing"
)

func TestDartOutputCarriesDocsAndDeprecations(t *testing.T) {
	file := compileDartSchema(t, `package app
/// A stored note.
///
/// Notes are immutable.
message Note {
  /// Unique identifier.
  id: string
  title: string @deprecated("use heading")
}
/// Visibility of a note.
enum Visibility {
  /// Only the author.
  PRIVATE
}
/// Manages notes.
service Notes {
  /// Fetch one note.
  get(Note) -> Note @get("/notes/{id}") @deprecated
}
`)
	models, client := string(GenerateTypes(file)), string(GenerateClient(file))
	for _, check := range []struct{ out, want string }{
		{models, "/// A stored note.\n///\n/// Notes are immutable.\nclass Note {"},
		{models, "  /// Unique identifier.\n  String id;"},
		{models, "  @Deprecated('use heading')\n  String title;"},
		{models, "/// Visibility of a note.\nenum Visibility {\n  /// Only the author.\n  private('PRIVATE');"},
		{models, "// ignore_for_file: deprecated_member_use_from_same_package"},
		{client, "/// Manages notes.\nclass NotesClient {"},
		{client, "  /// Fetch one note.\n  @Deprecated('no longer supported')\n  Future<Note> get("},
	} {
		if !strings.Contains(check.out, check.want) {
			t.Fatalf("missing %q in:\n%s", check.want, check.out)
		}
	}
}
