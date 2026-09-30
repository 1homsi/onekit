package gendart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const testPubspec = `name: onekit_check
environment:
  sdk: ^3.4.0
dependencies:
  http: ^1.2.0
  web_socket_channel: ^3.0.0
`

func compileDartSchema(t *testing.T, schema string) *onkir.File {
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

func writeDartFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dartCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("dart", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func dartPackage(t *testing.T, file *onkir.File) string {
	t.Helper()
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart not available")
	}
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	writeDartFile(t, filepath.Join(dir, "pubspec.yaml"), testPubspec)
	writeDartFile(t, filepath.Join(lib, "onekit.dart"), string(GenerateRuntime()))
	writeDartFile(t, filepath.Join(lib, "models.dart"), string(GenerateTypes(file)))
	if client := GenerateClient(file); client != nil {
		writeDartFile(t, filepath.Join(lib, "client.dart"), string(client))
	}
	if onkir.FileHasWSMethods(file) {
		writeDartFile(t, filepath.Join(lib, "onekit_ws.dart"), string(GenerateWSRuntime()))
		writeDartFile(t, filepath.Join(lib, "onekit_ws_io.dart"), string(GenerateWSConnectIO()))
		writeDartFile(t, filepath.Join(lib, "onekit_ws_web.dart"), string(GenerateWSConnectWeb()))
	}
	dartCommand(t, dir, "pub", "get")
	out := dartCommand(t, dir, "analyze", "--fatal-infos", "lib")
	if !strings.Contains(out, "No issues found") {
		t.Fatalf("dart analyze reported issues:\n%s", out)
	}
	return dir
}

func runDartSchema(t *testing.T, schema, main string) {
	t.Helper()
	dir := dartPackage(t, compileDartSchema(t, schema))
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), main)
	out := dartCommand(t, dir, "run", "bin/main.dart")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("dart run: %s", out)
	}
}
