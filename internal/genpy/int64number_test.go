package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestPythonInt64NumberEncodingIsOnTheWire(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onklang.Parse(`package app
message Item {
  id: int64
  big: uint64
  ids: int64[]
  maybe: int64?
  by_name: map[string, int64]
  keep: int64 @encode("number")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Int64Encoding: "number"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "main.py"), `
from models import Item

wire = {"id": 5, "big": 7, "ids": [1, 2], "maybe": 9, "by_name": {"a": 4}, "keep": 3}
item = Item.from_dict(wire)
item.validate()
assert item.to_dict() == wire, item.to_dict()
print("OK")
`)
	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
