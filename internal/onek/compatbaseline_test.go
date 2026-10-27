package onek

import (
	"path/filepath"
	"testing"
)

func TestCompatibilityBaselineWithoutConfigUsesCurrentConfig(t *testing.T) {
	schema := `
model R {
  id: string;
}

interface S {
  @get
  @route("/r/{id}")
  get(...R): R;
}
`
	current := t.TempDir()
	writeTestFile(t, filepath.Join(current, "onkt.toml"), "module = \"x\"\nroute_prefix = \"/api\"\nschema_root = \"schema\"\n")
	writeTestFile(t, filepath.Join(current, "schema", "api.tsp"), schema)
	previous := t.TempDir()
	writeTestFile(t, filepath.Join(previous, "schema", "api.tsp"), schema)
	findings, err := Compatibility(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("identical schemas reported changes: %+v", findings)
	}
}
