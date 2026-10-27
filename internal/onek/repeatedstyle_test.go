package onek

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRepeatedStyleValuesChangesGoTypes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/x\"\nrepeated_style = \"values\"\n\n[generate.go-server]\nout = \"gen\"\n")
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Row {
  id: string;
}

model List {
  rows: Row[];
}

model Q {
  @query
  limit: int32;
}

interface S {
  @get
  @route("/rows")
  list(...Q): List;
}
`)
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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/x\"\nrepeated_style = \"refs\"\n")
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model A {
  id: string;
}
`)
	if err := Check(dir); err == nil || !strings.Contains(err.Error(), "repeated_style") {
		t.Fatalf("expected a repeated_style error, got %v", err)
	}
}
