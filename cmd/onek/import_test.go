package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const importSpec = `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}}}}}}}`

func TestImportRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(spec, []byte(importSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "schemas")
	if err := run([]string{"import", "--out", out, spec}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	target := filepath.Join(out, "pets.onk")
	info, err := os.Stat(target)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Fatalf("imported file mode: %v %v", info, err)
	}
	if err := os.WriteFile(target, []byte("// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"import", "--out", out, spec}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second import overwrote edits: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "// edited\n" {
		t.Fatalf("edited file changed: %q", data)
	}
	if err := run([]string{"import", "--out", out, "--force", spec}); err != nil {
		t.Fatalf("forced import: %v", err)
	}
}

func TestImportRejectsSchemasThatFailCheck(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	broken := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets/{id}":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok"}}}}}}`
	if err := os.WriteFile(spec, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "schemas")
	err := run([]string{"import", "--out", out, spec})
	if err == nil || !strings.Contains(err.Error(), "does not pass onek check") {
		t.Fatalf("want check failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "pets.onk")); !os.IsNotExist(statErr) {
		t.Fatalf("broken schema was written: %v", statErr)
	}
}
