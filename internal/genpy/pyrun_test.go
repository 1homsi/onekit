package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func runPythonSchema(t *testing.T, schema, script string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "client.py"), string(GenerateClient(pkg.Files[0], "models")))
	writeFile(t, filepath.Join(dir, "main.py"), script)
	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("python run failed: %v\n%s", err, out)
	}
}

func TestPythonRequiredRejectsEmptyCollections(t *testing.T) {
	runPythonSchema(t, `
package app
message M {
  ids: string[] @required
  labels: map[string, string] @required
}
`, `
from models import M

try:
    M().validate()
    raise SystemExit("expected validation error")
except ValueError as error:
    assert "ids is required" in str(error), error
    assert "labels is required" in str(error), error
M(ids=["a"], labels={"k": "v"}).validate()
print("OK")
`)
}
