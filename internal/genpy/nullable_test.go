package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const nullableSchema = `package app

message Item { name: string }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable
  item: Item? @nullable
}
`

func TestPythonNullableFieldsKeepAbsentNullAndValueApart(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	ast, err := onklang.Parse(nullableSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "main.py"), `
from models import Patch, Item

assert Patch(id=1).to_dict() == {"id": 1}, Patch(id=1).to_dict()
assert Patch(id=1, folder_id_null=True, note_null=True, item_null=True).to_dict() == {"id": 1, "folder_id": None, "note": None, "item": None}
assert Patch(id=1, folder_id=7, note="n", item=Item(name="x")).to_dict() == {"id": 1, "folder_id": 7, "note": "n", "item": {"name": "x"}}
assert Patch(id=1, folder_id=7, folder_id_null=True).to_dict() == {"id": 1, "folder_id": 7}

decoded = Patch.from_dict({"id": 1, "folder_id": None, "note": "x"})
assert decoded.folder_id is None and decoded.folder_id_null is True
assert decoded.note == "x" and decoded.note_null is False
assert decoded.item is None and decoded.item_null is False
assert Patch.from_dict({"id": 1}).folder_id_null is False
assert Patch.from_dict(decoded.to_dict()).to_dict() == decoded.to_dict()
print("OK")
`)
	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
