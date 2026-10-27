package onek

import (
	"path/filepath"
	"testing"
)

func TestBuildWithSummaryCountsFilesAndTargets(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), onekitToml)
	writeTestFile(t, filepath.Join(dir, "models.tsp"), modelsTypeSpec)
	writeTestFile(t, filepath.Join(dir, "service.tsp"), serviceTypeSpec)
	summary, err := BuildWithSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Files < 5 || len(summary.Targets) != 3 || summary.Targets[0] != "go-server" || summary.Targets[2] != "openapi" {
		t.Fatalf("unexpected summary %+v", summary)
	}

	empty := t.TempDir()
	writeTestFile(t, filepath.Join(empty, "onkt.toml"), "module = \"x\"\n")
	writeTestFile(t, filepath.Join(empty, "api.tsp"), `
model M {}
`)
	summary, err = BuildWithSummary(empty)
	if err != nil || len(summary.Targets) != 0 {
		t.Fatalf("no-target build: %+v %v", summary, err)
	}
}
