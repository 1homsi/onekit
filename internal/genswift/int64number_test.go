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

func TestSwiftInt64NumberEncodingIsOnTheWire(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
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
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(pkg.Files[0])))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), `import Foundation

let wire = "{\"id\":5,\"big\":7,\"ids\":[1,2],\"maybe\":9,\"by_name\":{\"a\":4},\"keep\":3}"
let object = try JSONSerialization.jsonObject(with: Data(wire.utf8))
let item = try Item(json: object)
precondition(item.validate().isEmpty, "\(item.validate())")
let data = try JSONSerialization.data(withJSONObject: item.toJSONValue(), options: [.sortedKeys])
let text = String(decoding: data, as: UTF8.self)
precondition(text == "{\"big\":7,\"by_name\":{\"a\":4},\"id\":5,\"ids\":[1,2],\"keep\":3,\"maybe\":9}", text)
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
