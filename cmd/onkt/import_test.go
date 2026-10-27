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
	target := filepath.Join(out, "pets.tsp")
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
	broken := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{"Interface":{"type":"object","properties":{"a":{"type":"string"}}}}},"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Interface"}}}}}}}}}`
	if err := os.WriteFile(spec, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "schemas")
	err := run([]string{"import", "--out", out, spec})
	if err == nil || !strings.Contains(err.Error(), "does not pass onkt check") {
		t.Fatalf("want check failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "pets.tsp")); !os.IsNotExist(statErr) {
		t.Fatalf("broken schema was written: %v", statErr)
	}
}

func TestImportProducesAProjectThatBuilds(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(spec, []byte(importSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "project")
	if err := run([]string{"import", "--out", out, spec}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "main.tsp")); err != nil {
		t.Fatalf("expected a main.tsp importing the schema: %v", err)
	}
	linkTypeSpec(t, out)
	config := "module = \"example.com/pets\"\n\n[generate.go-server]\nout = \"./gen\"\n\n[generate.ts-client]\nout = \"./web\"\n"
	if err := os.WriteFile(filepath.Join(out, "onkt.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"build", "--dir", out}); err != nil {
		t.Fatalf("an imported schema must build: %v", err)
	}
	for _, name := range []string{"gen/server.gen.go", "web/client.ts"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestImportRejectsProtocolBuffers(t *testing.T) {
	dir := t.TempDir()
	proto := filepath.Join(dir, "shop.proto")
	if err := os.WriteFile(proto, []byte(`syntax = "proto3"; message M { string a = 1; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"import", "--out", filepath.Join(dir, "x"), proto}); err == nil || !strings.Contains(err.Error(), "no longer imports Protocol Buffers") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// linkTypeSpec gives a test project the repository's TypeSpec toolchain,
// skipping the test when `npm install` has not run at the repository root.
func linkTypeSpec(t *testing.T, dir string) {
	t.Helper()
	modules, err := filepath.Abs(filepath.Join("..", "..", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modules, ".bin", "tsp")); err != nil {
		t.Skip("TypeSpec is not installed; run `npm install` at the repository root")
	}
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		// Windows needs a privilege to create symlinks.
		t.Skipf("link the TypeSpec toolchain: %v", err)
	}
}
