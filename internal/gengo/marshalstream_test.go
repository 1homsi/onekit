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

const marshalStreamSchema = `package app

message Row {
  id: string
  name: string
  score: int32
  tag: string
}

message List {
  rows: Row[]
  labels: map[string, string]
  note: string
}
`

const marshalStreamProgram = `package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	app "example.com/stream/app"
)

type legacyList app.List

func (l *legacyList) MarshalJSON() ([]byte, error) { return (*app.List)(l).MarshalJSON() }

type plainRow struct {
	ID    string   ` + "`json:\"id\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
	Score int32    ` + "`json:\"score\"`" + `
	Tag string ` + "`json:\"tag\"`" + `
}

type plainList struct {
	Rows   []*plainRow       ` + "`json:\"rows\"`" + `
	Labels map[string]string ` + "`json:\"labels\"`" + `
	Note   string            ` + "`json:\"note\"`" + `
}

func build(n int) (*app.List, *plainList) {
	l := &app.List{Labels: map[string]string{"a": "b"}, Note: "n"}
	p := &plainList{Labels: map[string]string{"a": "b"}, Note: "n", Rows: []*plainRow{}}
	for i := 0; i < n; i++ {
		l.Rows = append(l.Rows, &app.Row{Id: fmt.Sprint("id", i), Name: "name", Score: int32(i), Tag: "t"})
		p.Rows = append(p.Rows, &plainRow{ID: fmt.Sprint("id", i), Name: "name", Score: int32(i), Tag: "t"})
	}
	return l, p
}

func main() {
	empty, err := json.Marshal(&app.List{})
	if err != nil { panic(err) }
	if string(empty) != ` + "`{\"note\":\"\",\"rows\":[],\"labels\":{}}`" + ` {
		fmt.Println("bad empty:", string(empty)); os.Exit(1)
	}
	l, p := build(200)
	want, err := json.Marshal((*legacyList)(l))
	if err != nil { panic(err) }
	got, err := json.Marshal(l)
	if err != nil { panic(err) }
	if !bytes.Equal(got, want) { fmt.Println("streaming output differs from the MarshalJSON output"); os.Exit(1) }
	nilWant, _ := json.Marshal((*legacyList)(&app.List{}))
	if !bytes.Equal(empty, nilWant) { fmt.Println("empty output differs"); os.Exit(1) }
	for _, c := range []struct{ name string; v any }{
		{"plain", p},
		{"legacy-wrapper", (*legacyList)(l)},
		{"streaming-wrapper", l},
	} {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := json.Marshal(c.v); err != nil { b.Fatal(err) }
			}
		})
		fmt.Printf("BENCH %s %d ns/op %d B/op\n", c.name, r.NsPerOp(), r.AllocedBytesPerOp())
	}
	fmt.Println("ok")
}
`

func TestStreamingMarshalMatchesPlainAndLegacyBytes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(marshalStreamSchema)
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
	if !strings.Contains(string(types), "MarshalJSONTo(enc *jsontext.Encoder)") {
		t.Fatalf("generated types lack the streaming marshal method:\n%s", types)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/stream\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), marshalStreamProgram)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	t.Logf("\n%s", out)
}
