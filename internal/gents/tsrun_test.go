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

func compileTSSchema(t *testing.T, schema string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func runTSSchema(t *testing.T, schema, main string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	file := compileTSSchema(t, schema)
	dir := t.TempDir()
	forNode := func(src []byte) string {
		return strings.ReplaceAll(string(src), `from "./types.js"`, `from "./types.ts"`)
	}
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), forNode(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), forNode(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), main)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
}

func TestTSFormatValidatorsSkipEmptyStrings(t *testing.T) {
	runTSSchema(t, `
package app
message Contact {
  email: string @email
  id: string @uuid
  site: string @uri
  code: string @pattern("^[A-Z]+$")
}
`, `
import { validateContact } from "./types.ts";

const empty = validateContact({ email: "", id: "", site: "", code: "" });
if (empty.length !== 0) throw new Error("empty strings failed format checks: " + empty.join(", "));
const bad = validateContact({ email: "nope", id: "x", site: "y", code: "z" });
if (bad.length !== 4) throw new Error("expected four violations, got " + bad.join(", "));
console.log("OK");
`)
}
