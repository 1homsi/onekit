package gents

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

func TestTSEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
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
