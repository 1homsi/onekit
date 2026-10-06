package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const principalRequiredSchema = `package app

message Principal @principal { user_id: string }
message Req { id: string }
message Ack { ok: bool }

service Docs {
  base_path: "/v1"
  open(Req) -> Ack @post("/open") @authorize("req.id != ''", "an id is required")
}
`

const principalRequiredHarness = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	app "example.com/principalrequired/app"
)

type impl struct{}

func (impl) Open(ctx context.Context, req *app.Req) (*app.Ack, error) { return &app.Ack{Ok: true}, nil }

func status(resolve func(context.Context, *http.Request) (*app.Principal, error)) int {
	mux := http.NewServeMux()
	if err := app.RegisterDocsServer(mux, impl{}, app.WithPrincipal(resolve)); err != nil { panic(err) }
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/open", "application/json", strings.NewReader("{\"id\":\"x\"}"))
	if err != nil { panic(err) }
	response.Body.Close()
	return response.StatusCode
}

func main() {
	if got := status(func(context.Context, *http.Request) (*app.Principal, error) { return nil, nil }); got != 401 {
		panic(fmt.Sprint("an unidentified caller must be 401 even when no rule reads auth, got ", got))
	}
	if got := status(func(context.Context, *http.Request) (*app.Principal, error) { return &app.Principal{UserId: "u"}, nil }); got != 200 {
		panic(fmt.Sprint("an identified caller passes, got ", got))
	}
	fmt.Println("OK")
}
`

func TestGoUnidentifiedCallerIsRejectedEvenWhenNoRuleReadsAuth(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, principalRequiredSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/principalrequired\n\ngo 1.26\n")
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
	writeFile(t, filepath.Join(dir, "main.go"), principalRequiredHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
