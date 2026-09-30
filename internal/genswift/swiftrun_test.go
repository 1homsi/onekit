package genswift

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileSwiftSchema(t *testing.T, schema string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func writeSwiftFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runSwiftSchema(t *testing.T, schema, main string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	file := compileSwiftSchema(t, schema)
	dir := t.TempDir()
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(file)))
	files := []string{"Onekit.swift", "Models.swift"}
	if client := GenerateClient(file); client != nil {
		writeSwiftFile(t, filepath.Join(dir, "Client.swift"), string(client))
		files = append(files, "Client.swift")
	}
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), main)
	files = append(files, "main.swift")
	bin := filepath.Join(dir, "check")
	compile := exec.Command("swiftc", append([]string{"-warnings-as-errors", "-o", bin}, files...)...)
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("run output: %s", out)
	}
}
