package onek

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRepeatedStyleValuesChangesGoTypes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/x\"\nrepeated_style = \"values\"\n\n[generate.go-server]\nout = \"gen\"\n")
	writeTestFile(t, filepath.Join(dir, "api.onk"), "message Row { id: string }\nmessage List { rows: Row[] }\nmessage Q { limit: int32 @query }\nservice S { list(Q) -> List @get(\"/rows\") }\n")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	types := read(t, filepath.Join(dir, "gen", "types.gen.go"))
	if !strings.Contains(types, "Rows []Row") || strings.Contains(types, "[]*Row") {
		t.Fatalf("repeated_style = values must produce []Row:\n%s", types)
	}
}

func TestRepeatedStyleRejectsUnknownValues(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/x\"\nrepeated_style = \"refs\"\n")
	writeTestFile(t, filepath.Join(dir, "api.onk"), "message A { id: string }\n")
	if err := Check(dir); err == nil || !strings.Contains(err.Error(), "repeated_style") {
		t.Fatalf("expected a repeated_style error, got %v", err)
	}
}
