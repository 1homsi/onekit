package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileWithInt64Number(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.onk", AST: ast}}, onkcompile.CompileOptions{Int64Encoding: "number"})
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Files[0]
}

func TestTSInt64NumberEncodingIsOnTheWire(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	file := compileWithInt64Number(t, `package app
message Item {
  id: int64
  big: uint64
  ids: int64[]
  maybe: int64?
  by_name: map[string, int64]
  keep: int64 @encode("number")
}
`)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { decodeItem, encodeItem, validateItem } from "./types.ts";

const wire = { id: 5, big: 7, ids: [1, 2], maybe: 9, by_name: { a: 4 }, keep: 3 };
const item = decodeItem(wire);
const problems = validateItem(item);
if (problems.length !== 0) throw new Error("numbers were rejected: " + problems.join(", "));
const out = JSON.stringify(encodeItem(item));
const expected = JSON.stringify(wire);
const sorted = (text: string) => JSON.stringify(Object.entries(JSON.parse(text)).sort());
if (sorted(out) !== sorted(expected)) throw new Error("wire " + out + ", want " + expected);
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("%v\n%s", err, out)
	}
}
