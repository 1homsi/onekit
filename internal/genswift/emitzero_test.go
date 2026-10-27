package genswift

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const emitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  @encode(string)\n  huge: uint64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

func TestSwiftEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
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
