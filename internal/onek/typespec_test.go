package onek

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoNodeModules returns the repository's npm workspace, where the
// @onkt/typespec emitter and the TypeSpec compiler are installed, skipping the
// test when `npm install` has not run.
func repoNodeModules(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(root, "node_modules")
	if _, err := os.Stat(filepath.Join(modules, ".bin", "tsp")); err != nil {
		t.Skip("TypeSpec is not installed; run `npm install` at the repository root")
	}
	if _, err := os.Stat(filepath.Join(root, "typespec", "dist", "src", "index.js")); err != nil {
		t.Skip("the @onkt/typespec emitter is not built; run `npm run build --workspace typespec`")
	}
	return modules
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestTypeSpecExampleMatchesOnk builds examples/onk-simple-api from a
// TypeSpec port of its schema and requires every generated file to match the
// output checked in from the .onk schema byte for byte.
func TestTypeSpecExampleMatchesOnk(t *testing.T) {
	modules := repoNodeModules(t)
	example := filepath.Join("..", "..", "examples", "onk-simple-api")
	dir := t.TempDir()
	copyTree(t, example, dir)
	for _, name := range []string{"models.onk", "service.onk"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(t, filepath.Join("testdata", "typespec-simple"), dir)
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONEK_NO_CACHE", "1")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"api/types.gen.go", "api/validate.gen.go", "api/server.gen.go", "api/client.gen.go", "docs/openapi.yaml", "docs/openapi.json"} {
		want, err := os.ReadFile(filepath.Join(example, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the output of the .onk schema", rel)
		}
	}
}

func TestTypeSpecDiagnosticsReachTheCaller(t *testing.T) {
	modules := repoNodeModules(t)
	dir := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tsp"), []byte("import \"@onkt/typespec\";\nmodel User { id: Missing; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Fatalf("err = %v, want the TypeSpec diagnostic naming Missing", err)
	}
}

func TestTypeSpecProjectNeedsMainTsp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models.tsp"), []byte("model User {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "main.tsp") {
		t.Fatalf("err = %v, want a hint about main.tsp", err)
	}
}
