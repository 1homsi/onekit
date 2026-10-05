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

const emitZeroSchema = `package app

enum Level { LOW HIGH }

message Inner { v: string }

message Item {
  name: string
  flag: bool
  count: int32
  big: int64
  huge: uint64
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

func TestSwiftEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
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
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(pkg.Files[0])))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), `import Foundation

func canon(_ value: Any) -> String {
    let data = try! JSONSerialization.data(withJSONObject: value, options: [.sortedKeys])
    return String(decoding: data, as: UTF8.self)
}

let zero = Item().toJSON()
let want = "{\"big\":\"0\",\"by_name\":{},\"count\":0,\"data\":\"\",\"flag\":false,\"hex\":\"\",\"huge\":\"0\",\"ids\":[],\"items\":[],\"level\":\"LOW\",\"name\":\"\",\"num_level\":0,\"ratio\":0,\"tags\":[]}"
precondition(canon(zero) == want, canon(zero))
let set = Item(name: "n", maybe: "x").toJSON()
precondition(set["name"] as? String == "n" && set["maybe"] as? String == "x", "\(set)")
precondition(set["maybe_n"] == nil && set["inner"] == nil, "\(set)")
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
