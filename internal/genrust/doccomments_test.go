package genrust

import (
	"strings"
	"testing"
)

const rustDocFixture = `
package app
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
`

func TestRustOutputCarriesDocComments(t *testing.T) {
	file := compileRustSchema(t, rustDocFixture)
	types, server, client := string(GenerateTypes(file)), string(GenerateServer(file)), string(GenerateClient(file))
	for _, check := range []struct{ out, want string }{
		{types, "/// A stored note.\n///\n/// Notes are immutable.\n#[derive("},
		{types, "    /// Unique identifier.\n    #[serde("},
		{types, "/// Visibility of a note.\n#[derive("},
		{types, "    /// Only the author.\n    #[serde(rename = \"PRIVATE\")]"},
		{server, "/// Manages notes.\npub trait Notes"},
		{server, "    /// Fetch one note.\n    fn get("},
		{client, "/// Manages notes.\n#[derive(Debug, Clone)]\npub struct NotesClient"},
		{client, "/// Fetch one note.\n    pub async fn get("},
	} {
		if !strings.Contains(check.out, check.want) {
			t.Fatalf("missing %q in:\n%s", check.want, check.out)
		}
	}
}

func TestGeneratedRustWithDocCommentsBuilds(t *testing.T) {
	runRustWSCrate(t, rustDocFixture, "onekit-rust-docs", "mod generated;\nfn main() { println!(\"OK\"); }\n", false)
}
