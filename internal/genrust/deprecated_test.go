package genrust

import (
	"strings"
	"testing"
)

const rustDeprecatedFixture = `
package app
message Note {
  id: string
  title: string @deprecated("use heading")
}
service Notes { get(Note) -> Note @get("/notes/{id}") @deprecated }
`

func TestRustMarksDeprecatedFieldsAndMethods(t *testing.T) {
	file := compileRustSchema(t, rustDeprecatedFixture)
	types, client := string(GenerateTypes(file)), string(GenerateClient(file))
	if !strings.Contains(types, "#[deprecated(note = \"use heading\")]\n    #[serde(") || !strings.Contains(client, "#[deprecated]\n    pub async fn get(") {
		t.Fatalf("missing deprecation attributes:\n%s\n%s", types, client)
	}
	runRustWSCrate(t, rustDeprecatedFixture, "onekit-rust-deprecated", "mod generated;\nfn main() { println!(\"OK\"); }\n", false)
}
