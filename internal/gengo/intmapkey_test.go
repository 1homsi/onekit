package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestGoIntegerMapKeysRoundTripAndRejectNonNumericKeys(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse("package app\nmessage R { by_id: map[int64, string]  by_n: map[uint32, string] }\n")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	types, err := GenerateTypesWithResolver(pkg.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "map[int64]string") || !strings.Contains(string(types), "map[uint32]string") {
		t.Fatalf("integer map keys must stay integers in Go:\n%s", types)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/keys\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"
	"os"

	app "example.com/keys/app"
)

func main() {
	out, err := json.Marshal(&app.R{ById: map[int64]string{-7: "a", 42: "b"}})
	if err != nil { panic(err) }
	fmt.Println(string(out))
	var back app.R
	if err := json.Unmarshal(out, &back); err != nil || back.ById[42] != "b" || back.ById[-7] != "a" { fmt.Println("round trip failed", err); os.Exit(1) }
	for _, bad := range []string{`+"`{\"by_id\":{\"abc\":\"x\"}}`, `{\"by_n\":{\"-1\":\"x\"}}`, `{\"by_id\":{\"1.5\":\"x\"}}`"+`} {
		var r app.R
		if err := json.Unmarshal([]byte(bad), &r); err == nil { fmt.Println("accepted", bad); os.Exit(1) }
	}
	fmt.Println("ok")
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") || !strings.Contains(string(out), `"by_id":{"-7":"a","42":"b"}`) {
		t.Fatalf("go run: %v\n%s", err, out)
	}
}
