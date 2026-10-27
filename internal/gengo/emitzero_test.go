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
	"github.com/1homsi/onekit/internal/onktest"
)

const emitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

func TestGoEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onktest.Parse(emitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
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
	ast, err := onktest.Parse(`namespace app;

model Names {
  @Onkt.unwrap
  names: string[];
}

model Tags {
  @Onkt.unwrap
  tags: Record<string>;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
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
