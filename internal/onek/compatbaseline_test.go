package onek

import (
	"path/filepath"
	"testing"
)

func TestCompatibilityBaselineWithoutConfigUsesCurrentConfig(t *testing.T) {
	schema := "message R { id: string }\nservice S { get(R) -> R @get(\"/r/{id}\") }\n"
	current := t.TempDir()
	writeTestFile(t, filepath.Join(current, "onekit.toml"), "module = \"x\"\nroute_prefix = \"/api\"\nschema_root = \"schema\"\n")
	writeTestFile(t, filepath.Join(current, "schema", "api.onk"), schema)
	previous := t.TempDir()
	writeTestFile(t, filepath.Join(previous, "schema", "api.onk"), schema)
	findings, err := Compatibility(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("identical schemas reported changes: %+v", findings)
	}
}
