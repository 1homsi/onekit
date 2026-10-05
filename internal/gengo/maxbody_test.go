package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const maxBodySchema = `package app

message Blob { data: string }
message Done { ok: bool }

service Uploads {
  small(Blob) -> Done @post("/small")
  big(Blob) -> Done @post("/big") @max_body("1MiB")
}
`

const maxBodyHarness = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	app "example.com/maxbody/app"
)

type impl struct{}

func (impl) Small(ctx context.Context, req *app.Blob) (*app.Done, error) { return &app.Done{Ok: true}, nil }
func (impl) Big(ctx context.Context, req *app.Blob) (*app.Done, error)   { return &app.Done{Ok: true}, nil }

func main() {
	mux := http.NewServeMux()
	if err := app.RegisterUploadsServer(mux, impl{}, app.WithMaxRequestBodyBytes(1024)); err != nil { panic(err) }
	server := httptest.NewServer(mux)
	defer server.Close()
	post := func(path string, size int) int {
		body := "{\"data\":\"" + strings.Repeat("a", size) + "\"}"
		response, err := http.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil { return 413 }
		response.Body.Close()
		return response.StatusCode
	}
	for _, c := range []struct{ path string; size, want int }{
		{"/small", 100, 200}, {"/small", 4096, 413},
		{"/big", 4096, 200}, {"/big", 2 << 20, 413},
	} {
		if got := post(c.path, c.size); got != c.want { panic(fmt.Sprintf("%s %d: %d want %d", c.path, c.size, got, c.want)) }
	}
	fmt.Println("OK")
}
`

func TestGoMaxBodyDecoratorOverridesTheServerLimitPerMethod(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, maxBodySchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/maxbody\n\ngo 1.26\n")
	for name, generate := range map[string]func() ([]byte, error){
		"types.go":    func() ([]byte, error) { return GenerateTypesWithResolver(file, nil) },
		"validate.go": func() ([]byte, error) { return GenerateValidationWithResolver(file, nil) },
		"server.go":   func() ([]byte, error) { return GenerateServerWithResolver(file, nil) },
	} {
		out, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", name), string(out))
	}
	writeFile(t, filepath.Join(dir, "main.go"), maxBodyHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
