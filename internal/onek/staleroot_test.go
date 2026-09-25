package onek

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRemovesOutputsLeftBehindWhenOutChanges(t *testing.T) {
	dir := t.TempDir()
	config := func(out string) string {
		return "module = \"example.com/api\"\n[generate.go-server]\nout = \"" + out + "\"\n"
	}
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), config("./old"))
	writeTestFile(t, filepath.Join(dir, "api.onk"), `
message Request {}
message Response {}
service API { get(Request) -> Response @get("/items") }
`)
	if err := Build(dir); err != nil {
		t.Fatalf("first Build error: %v", err)
	}
	stale := filepath.Join(dir, "old", "server.gen.go")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("expected initial generated file: %v", err)
	}
	userFile := filepath.Join(dir, "old", "handlers.go")
	writeTestFile(t, userFile, "package api\n")
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), config("./new"))
	if err := Build(dir); err != nil {
		t.Fatalf("second Build error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("output under the old out directory still exists: %v", err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("user file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new", "server.gen.go")); err != nil {
		t.Fatalf("new output missing: %v", err)
	}
}
