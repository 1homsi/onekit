package onek

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateExample migrates examples/onk-simple-api and requires the
// TypeSpec project to build to the output checked in from the .onk schema.
func TestMigrateExample(t *testing.T) {
	modules := repoNodeModules(t)
	example := filepath.Join("..", "..", "examples", "onk-simple-api")
	dir := t.TempDir()
	copyTree(t, example, dir)

	result, err := Migrate(dir, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.tsp", "service.tsp", "main.tsp", "package.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("migrate did not write %s: %v", name, err)
		}
	}
	for _, name := range []string{"models.onk", "service.onk"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("migrate left %s in place", name)
		}
	}
	if len(result.Removed) != 2 {
		t.Errorf("removed = %v", result.Removed)
	}

	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONEK_NO_CACHE", "1")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"api/types.gen.go", "api/validate.gen.go", "api/server.gen.go", "api/client.gen.go", "docs/openapi.yaml", "docs/openapi.json"} {
		want, _ := os.ReadFile(filepath.Join(example, rel))
		got, _ := os.ReadFile(filepath.Join(dir, rel))
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the output of the .onk schema", rel)
		}
	}
}

func TestMigrateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	writeTestFile(t, filepath.Join(dir, "api.tsp"), "// mine\n")
	_, err := Migrate(dir, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal naming --force", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("a refused migration must not remove the .onk file")
	}
}

func TestMigrateKeepsSchemasOnRequest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	if _, err := Migrate(dir, MigrateOptions{KeepSchemas: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("--keep must leave the .onk file in place")
	}
	main, err := os.ReadFile(filepath.Join(dir, "main.tsp"))
	if err != nil || !strings.Contains(string(main), `import "./api.tsp";`) {
		t.Fatalf("main.tsp = %q, %v", main, err)
	}
}
