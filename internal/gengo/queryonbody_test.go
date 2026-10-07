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

const queryOnBodySchema = `package app

message Save {
  id: string
  name: string
  dry_run: bool @query
  tags: string[] @query("tag")
}
message Saved { id: string  name: string  dry_run: bool  tags: string[] }

service Things {
  base_path: "/v1"
  save(Save) -> Saved @put("/things/{id}")
}
`

const queryOnBodyHarness = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	app "example.com/qb/app"
)

type impl struct{}

func (impl) Save(ctx context.Context, req *app.Save) (*app.Saved, error) {
	return &app.Saved{Id: req.Id, Name: req.Name, DryRun: req.DryRun, Tags: req.Tags}, nil
}

func main() {
	mux := http.NewServeMux()
	if err := app.RegisterThingsServer(mux, impl{}); err != nil { panic(err) }
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var seen string
	client := app.NewThingsClient(srv.URL)
	client.HTTPClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		seen = r.URL.String()
		return http.DefaultTransport.RoundTrip(r)
	})}
	out, err := client.Save(context.Background(), &app.Save{Id: "7", Name: "n", DryRun: true, Tags: []string{"a", "b"}})
	if err != nil { panic(err) }
	if !strings.Contains(seen, "dry_run=true") || !strings.Contains(seen, "tag=a") || !strings.Contains(seen, "tag=b") { fmt.Println("query missing:", seen); os.Exit(1) }
	if !out.DryRun || strings.Join(out.Tags, ",") != "a,b" || out.Name != "n" || out.Id != "7" { fmt.Printf("round trip: %+v\n", out); os.Exit(1) }
	fmt.Println("OK")
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
`

func TestGoQueryParamsOnBodyBearingRoutes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(queryOnBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server", "go-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidationWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateServerWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := GenerateClientWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/qb\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "app", "client.go"), string(client))
	writeFile(t, filepath.Join(dir, "main.go"), queryOnBodyHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestQueryOnBodyBearingRouteNeedsTargetsThatSupportIt(t *testing.T) {
	ast, err := onklang.Parse(queryOnBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	_, err = onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server", "python-client"}})
	if err == nil || !strings.Contains(err.Error(), "go, ts and openapi targets only") {
		t.Fatalf("expected the unsupported-target error, got %v", err)
	}
}
