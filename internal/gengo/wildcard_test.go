package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wildcardSchema = `package app

message FileRef { path: string }
message Content { path: string }
message Empty {}

service Files {
  base_path: "/files"
  read(FileRef) -> Content @get("/{path...}")
  root(Empty) -> Content @get("")
}
`

const wildcardHarness = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	app "example.com/wild/app"
)

type impl struct{}

func (impl) Read(ctx context.Context, req *app.FileRef) (*app.Content, error) {
	return &app.Content{Path: req.Path}, nil
}
func (impl) Root(ctx context.Context, req *app.Empty) (*app.Content, error) {
	return &app.Content{Path: "ROOT"}, nil
}

func get(url string) string {
	response, err := http.Get(url)
	if err != nil { panic(err) }
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return fmt.Sprintf("%d %s", response.StatusCode, body)
}

func main() {
	mux := http.NewServeMux()
	if err := app.RegisterFilesServer(mux, impl{}); err != nil { panic(err) }
	server := httptest.NewServer(mux)
	defer server.Close()
	for path, want := range map[string]string{
		"/files/a.txt":          "200 {\"path\":\"a.txt\"}\n",
		"/files/dir/sub/b.txt":  "200 {\"path\":\"dir/sub/b.txt\"}\n",
		"/files/sp%20ace/x%2By": "200 {\"path\":\"sp ace/x+y\"}\n",
		"/files":                "200 {\"path\":\"ROOT\"}\n",
	} {
		if got := get(server.URL + path); got != want {
			panic(fmt.Sprintf("%s: %q, want %q", path, got, want))
		}
	}
	client := app.NewFilesClient(server.URL)
	for _, path := range []string{"a.txt", "dir/sub/b.txt", "sp ace/x+y", "100%/ü.txt"} {
		got, err := client.Read(context.Background(), &app.FileRef{Path: path})
		if err != nil || got.Path != path { panic(fmt.Sprintf("client %q: %v %v", path, got, err)) }
	}
	root, err := client.Root(context.Background(), &app.Empty{})
	if err != nil || root.Path != "ROOT" { panic(fmt.Sprintf("root: %v %v", root, err)) }
	fmt.Println("OK")
}
`

func TestGoWildcardPathsAndEmptyRoutes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, wildcardSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/wild\n\ngo 1.26\n")
	for name, generate := range map[string]func() ([]byte, error){
		"types.go":    func() ([]byte, error) { return GenerateTypesWithResolver(file, nil) },
		"validate.go": func() ([]byte, error) { return GenerateValidationWithResolver(file, nil) },
		"server.go":   func() ([]byte, error) { return GenerateServerWithResolver(file, nil) },
		"client.go":   func() ([]byte, error) { return GenerateClientWithResolver(file, nil) },
	} {
		out, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", name), string(out))
	}
	writeFile(t, filepath.Join(dir, "main.go"), wildcardHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
