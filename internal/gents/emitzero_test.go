package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const emitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

func TestTSEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
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
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(pkg.Files[0])))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { encodeItem } from "./types.ts";

const sorted = (value: unknown) => JSON.stringify(Object.entries(value as Record<string, unknown>).sort());
const zero = {
  name: "", flag: false, count: 0, big: "0", ratio: 0, level: "LOW", num_level: 0,
  tags: [], ids: ["0"].slice(1), items: [], by_name: {}, data: "", hex: "",
};
const got = encodeItem({} as any);
if (sorted(got) !== sorted(zero)) throw new Error("zero value wire " + JSON.stringify(got) + ", want " + JSON.stringify(zero));

const set = encodeItem({ name: "n", flag: true, count: 2, big: "9", level: "HIGH", numLevel: "HIGH", tags: ["a"], maybe: "x" } as any);
if (set.name !== "n" || set.flag !== true || set.count !== 2 || set.big !== "9" || set.level !== "HIGH" || set.maybe !== "x") {
  throw new Error("set values must pass through: " + JSON.stringify(set));
}
if ("maybe_n" in set || "inner" in set) throw new Error("unset optionals and messages must stay omitted: " + JSON.stringify(set));
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
