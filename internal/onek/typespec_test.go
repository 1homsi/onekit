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

// linkTypeSpec gives a test project the repository's TypeSpec toolchain.
func linkTypeSpec(t *testing.T, dir string) {
	t.Helper()
	modules := repoNodeModules(t)
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		// Windows needs a privilege to create symlinks.
		t.Skipf("link the TypeSpec toolchain: %v", err)
	}
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

// exampleOutputs are the generated files examples/simple-api checks in.
var exampleOutputs = []string{"api/types.gen.go", "api/validate.gen.go", "api/server.gen.go", "api/client.gen.go", "docs/openapi.yaml", "docs/openapi.json"}

// requireExampleOutputs fails unless dir generated exactly the files
// examples/simple-api checks in.
func requireExampleOutputs(t *testing.T, dir string) {
	t.Helper()
	example := filepath.Join("..", "..", "examples", "simple-api")
	for _, rel := range exampleOutputs {
		want, err := os.ReadFile(filepath.Join(example, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from examples/simple-api", rel)
		}
	}
}

// TestTypeSpecExampleBuilds builds examples/simple-api from its TypeSpec
// sources and requires the checked-in output.
func TestTypeSpecExampleBuilds(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "examples", "simple-api"), dir)
	linkTypeSpec(t, dir)
	t.Setenv("ONEK_NO_CACHE", "1")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	requireExampleOutputs(t, dir)
}

func TestTypeSpecDiagnosticsReachTheCaller(t *testing.T) {
	dir := t.TempDir()
	linkTypeSpec(t, dir)
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

func TestTypeSpecErrorsBecomeDiagnostics(t *testing.T) {
	err := &TypeSpecError{Output: "Diagnostics were reported during compilation:\n\nmain.tsp:3:14 - error unknown-identifier: Unknown identifier Missing\nmain.tsp:1:1 - warning deprecated: old\n\nFound 1 error."}
	got := Diagnostics(err)
	if len(got) != 1 || got[0].Path != "main.tsp" || got[0].Line != 3 || got[0].Column != 14 || got[0].Code != "unknown-identifier" || got[0].Message != "Unknown identifier Missing" {
		t.Fatalf("diagnostics = %+v", got)
	}
}
