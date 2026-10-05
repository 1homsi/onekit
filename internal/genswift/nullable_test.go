package genswift

import (
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestSwiftNullableFieldsKeepAbsentNullAndValueApart(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
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
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(pkg.Files[0])))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), `import Foundation

func canon(_ value: Any) -> String {
    let data = try! JSONSerialization.data(withJSONObject: value, options: [.sortedKeys])
    return String(decoding: data, as: UTF8.self)
}

precondition(canon(Patch(id: 1).toJSON()) == "{\"id\":1}", canon(Patch(id: 1).toJSON()))
let nulls = Patch(id: 1, folderIdNull: true, noteNull: true, itemNull: true).toJSON()
precondition(canon(nulls) == "{\"folder_id\":null,\"id\":1,\"item\":null,\"note\":null}", canon(nulls))
let values = Patch(id: 1, folderId: 7, note: "n", item: Item(name: "x")).toJSON()
precondition(values["folder_id"] as? Int64 == 7 && values["note"] as? String == "n", "\(values)")
let both = Patch(id: 1, note: "n", noteNull: true).toJSON()
precondition(both["note"] as? String == "n", "\(both)")

let decoded = try! Patch(json: ["id": 1, "folder_id": NSNull(), "note": "x"])
precondition(decoded.folderIdNull && decoded.folderId == nil)
precondition(!decoded.noteNull && decoded.note == "x")
precondition(!decoded.itemNull && decoded.item == nil)
precondition(!(try! Patch(json: ["id": 1])).folderIdNull)
print("OK")
`)
	bin := filepath.Join(dir, "check")
	compile := exec.Command("swiftc", "-warnings-as-errors", "-o", bin, "Onekit.swift", "Models.swift", "main.swift")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
