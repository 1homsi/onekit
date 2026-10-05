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

func TestGoEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(emitZeroSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/zero\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"

	app "example.com/zero/app"
)

func main() {
	zero, err := json.Marshal(&app.Item{})
	if err != nil { panic(err) }
	fmt.Println(string(zero))
	var back app.Item
	if err := json.Unmarshal(zero, &back); err != nil { panic(err) }
	again, _ := json.Marshal(&back)
	fmt.Println(string(again))
	text := "x"
	full, err := json.Marshal(&app.Item{Name: "n", Flag: true, Count: 2, Big: 9, Ratio: 1.5, Level: app.LevelHigh, NumLevel: app.LevelHigh, Tags: []string{"a"}, Ids: []int64{1}, Items: []*app.Inner{{V: "q"}}, ByName: map[string]string{"k": "v"}, Data: []byte("hi"), Hex: []byte{255}, Maybe: &text, Inner: &app.Inner{V: "i"}})
	if err != nil { panic(err) }
	fmt.Println(string(full))
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		t.Fatalf("unexpected output:\n%s", out)
	}
	zero := map[string]any{
		"name": "", "flag": false, "count": 0.0, "big": "0", "ratio": 0.0, "level": "LOW", "num_level": 0.0,
		"tags": []any{}, "ids": []any{"0"}[:0], "items": []any{}, "by_name": map[string]any{}, "data": "", "hex": "",
	}
	for i, line := range lines[:2] {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, zero) {
			t.Fatalf("zero value line %d:\n got  %s\n want %v", i, line, zero)
		}
	}
	var full map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &full); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"name": "n", "flag": true, "count": 2.0, "big": "9", "level": "HIGH", "num_level": 1.0, "maybe": "x", "hex": "ff", "data": "aGk="} {
		if !reflect.DeepEqual(full[key], want) {
			t.Errorf("%s = %v, want %v", key, full[key], want)
		}
	}
	if _, ok := full["maybe_n"]; ok {
		t.Errorf("an unset optional must still be omitted: %s", lines[2])
	}
}

func TestGoEmitZeroValuesKeepsRootUnwrapListsNonNull(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse("package app\nmessage Names { names: string[] @unwrap }\nmessage Tags { tags: map[string, string] @unwrap }\n")
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/zero\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"

	app "example.com/zero/app"
)

func main() {
	a, _ := json.Marshal(&app.Names{})
	b, _ := json.Marshal(&app.Tags{})
	fmt.Println(string(a), string(b))
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "[] {}" {
		t.Fatalf("%v\n%s", err, out)
	}
}
