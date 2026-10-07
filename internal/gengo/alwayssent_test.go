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

const alwaysSentSchema = `package app

message Inner { label: string }
message Node { name: string  next: Node }
message Env {
  id: string
  created_at: timestamp
  inner: Inner
  node: Node
  spare: Inner?
}
message Get { id: string }
service Envs {
  base_path: "/v1"
  get(Get) -> Env @get("/envs/{id}")
}
`

func TestGoWritesUnsetResponseMessageFieldsAsZeroMessages(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(alwaysSentSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
	if err != nil {
		t.Fatal(err)
	}
	types, err := GenerateTypesWithResolver(pkg.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/sent\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"

	app "example.com/sent/app"
)

func main() {
	out, err := json.Marshal(&app.Env{})
	if err != nil { panic(err) }
	fmt.Println(string(out))
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{`"created_at":"0001-01-01T00:00:00Z"`, `"inner":{"label":""}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, `"spare"`) || strings.Contains(got, `"node"`) && strings.Contains(got, `"node":{"name":"","next"`) {
		t.Errorf("optional and self-referencing fields must stay omitted when unset: %s", got)
	}
}
