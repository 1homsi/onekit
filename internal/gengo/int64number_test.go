package gengo

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const int64NumberSchema = `package app

message Item {
  id: int64
  big: uint64
  ids: int64[]
  maybe: int64?
  by_name: map[string, int64]
  keep: int64 @encode("number")
}
`

func compileWithInt64Number(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Int64Encoding: "number"})
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Files[0]
}

func TestGoInt64NumberEncodingIsOnTheWire(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileWithInt64Number(t, int64NumberSchema)
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/i64\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"

	app "example.com/i64/app"
)

func main() {
	nine := int64(9)
	out, err := json.Marshal(&app.Item{Id: 5, Big: 7, Ids: []int64{1, 2}, Maybe: &nine, ByName: map[string]int64{"a": 4}, Keep: 3})
	if err != nil { panic(err) }
	fmt.Println(string(out))
	var in app.Item
	if err := json.Unmarshal([]byte(`+"`"+`{"id":5,"big":7,"ids":[1,2],"maybe":9,"by_name":{"a":4},"keep":3}`+"`"+`), &in); err != nil { panic(err) }
	again, _ := json.Marshal(&in)
	fmt.Println(string(again))
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := map[string]any{"id": 5.0, "big": 7.0, "ids": []any{1.0, 2.0}, "maybe": 9.0, "by_name": map[string]any{"a": 4.0}, "keep": 3.0}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("wire JSON %s, want numbers everywhere: %v", line, want)
		}
	}
}
