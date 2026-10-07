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

const repeatedValuesSchema = `package app

message Row {
  id: string
  name: string @len(1, 20)
  tags: string[]
}

message List {
  rows: Row[] @max_items(500)
  @rule("self.rows.all(r, r.id != '')", "every row needs an id")
}

message Save {
  rows: Row[]
  note: string
}

message Query { limit: int32 @query }

service Rows {
  base_path: "/v1"
  list(Query) -> List @get("/rows")
  save(Save) -> List @post("/rows")
}
`

const repeatedValuesHarness = `package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	app "example.com/vals/app"
)

type impl struct{}

func (impl) List(ctx context.Context, req *app.Query) (*app.List, error) {
	return &app.List{Rows: []app.Row{{Id: "1", Name: "a", Tags: []string{"x"}}, {Id: "2", Name: "b"}}}, nil
}

func (impl) Save(ctx context.Context, req *app.Save) (*app.List, error) {
	return &app.List{Rows: req.Rows}, nil
}

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func main() {
	var empty app.List
	data, _ := json.Marshal(&empty)
	if string(data) != "{\"rows\":[]}" { fail("nil rows must marshal as []: %s", data) }

	mux := http.NewServeMux()
	if err := app.RegisterRowsServer(mux, impl{}); err != nil { panic(err) }
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/rows", nil))
	var list app.List
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil { panic(err) }
	if rec.Code != 200 || len(list.Rows) != 2 || list.Rows[0].Tags[0] != "x" { fail("list: %d %s", rec.Code, rec.Body) }

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/rows", bytes.NewReader([]byte("{\"rows\":[{\"id\":\"1\",\"name\":\"\"}]}"))))
	if rec.Code != 400 { fail("an invalid row must be rejected, got %d %s", rec.Code, rec.Body) }

	bad := &app.List{Rows: []app.Row{{Id: "", Name: "a"}}}
	if err := bad.Validate(); err == nil { fail("the @rule must reject a row without an id") }
	good := &app.List{Rows: []app.Row{{Id: "1", Name: "a"}}}
	if err := good.Validate(); err != nil { fail("valid list rejected: %v", err) }
	fmt.Println("OK")
}
`

func TestGoRepeatedValuesStyleGeneratesSlicesOfValues(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(repeatedValuesSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true, RepeatedValues: true})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "Rows []Row") || strings.Contains(string(types), "[]*Row") {
		t.Fatalf("repeated messages must be values:\n%s", types)
	}
	validation, err := GenerateValidationWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateServerWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/vals\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), repeatedValuesHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestGoRepeatedPointersStayTheDefault(t *testing.T) {
	file := compileFixtureSource(t, repeatedValuesSchema)
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "[]*Row") {
		t.Fatalf("the default must stay []*T:\n%s", types)
	}
}
