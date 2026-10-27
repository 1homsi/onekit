package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const emitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

func TestPythonEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onktest.Parse(emitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
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
