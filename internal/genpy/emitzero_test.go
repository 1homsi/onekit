package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const emitZeroSchema = `package app

enum Level { LOW HIGH }

message Inner { v: string }

message Item {
  name: string
  flag: bool
  count: int32
  big: int64
  ratio: float64
  level: Level
  num_level: Level @encode("number")
  tags: string[]
  ids: int64[]
  items: Inner[]
  by_name: map[string, string]
  data: bytes
  hex: bytes @encode("hex")
  maybe: string?
  maybe_n: int32?
  inner: Inner
}
`

func TestPythonEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onklang.Parse(emitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "main.py"), `
from models import Item, Level, Inner

zero = {"name": "", "flag": False, "count": 0, "big": "0", "ratio": 0.0, "level": "LOW", "num_level": 0,
        "tags": [], "ids": [], "items": [], "by_name": {}, "data": "", "hex": ""}
assert Item().to_dict() == zero, Item().to_dict()
assert Item.from_dict(zero).to_dict() == zero

full = Item(name="n", flag=True, count=2, big=9, level=Level.HIGH, tags=["a"], maybe="x", inner=Inner(v="i"))
d = full.to_dict()
assert d["name"] == "n" and d["flag"] is True and d["count"] == 2 and d["big"] == "9" and d["level"] == "HIGH", d
assert d["maybe"] == "x" and "maybe_n" not in d, d
print("OK")
`)
	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
