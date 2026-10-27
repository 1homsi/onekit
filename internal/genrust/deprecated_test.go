package genrust

import (
	"strings"
	"testing"
)

const rustDeprecatedFixture = `namespace app;

model Note {
  id: string;
  #deprecated "use heading"
  title: string;
}

interface Notes {
  #deprecated ""
  @get
  @route("/notes/{id}")
  get(...Note): Note;
}
`

func TestRustMarksDeprecatedFieldsAndMethods(t *testing.T) {
	t.Parallel()
	file := compileRustSchema(t, rustDeprecatedFixture)
	types, client := string(GenerateTypes(file)), string(GenerateClient(file))
	if !strings.Contains(types, "#[deprecated(note = \"use heading\")]\n    #[serde(") || !strings.Contains(client, "#[deprecated]\n    pub async fn get(") {
		t.Fatalf("missing deprecation attributes:\n%s\n%s", types, client)
	}
	runRustWSCrate(t, rustDeprecatedFixture, "onekit-rust-deprecated", "mod generated;\nfn main() { println!(\"OK\"); }\n", false)
}
