package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const httpkitSchema = `package app

message Thing { id: int64 @encode("number")  name: string }
message GetThing { id: int64 @encode("number") }

service Things {
  base_path: "/v1"
  get(GetThing) -> Thing @get("/things/{id}") @guard("things/read/:id", "audit")
  fail(GetThing) -> Thing @get("/fail/{id}")
}
`

const httpkitHarness = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/1homsi/onekit/httpkit"

	app "example.com/kit/app"
)

type impl struct{}

func (impl) Get(ctx context.Context, req *app.GetThing) (*app.Thing, error) {
	if p := httpkit.MustPrincipal[string](ctx); p != "alice" {
		panic("principal lost: " + p)
	}
	return &app.Thing{Id: req.Id, Name: "thing"}, nil
}

func (impl) Fail(ctx context.Context, req *app.GetThing) (*app.Thing, error) {
	return nil, errors.New("db exploded: secret")
}

type observer struct{ result app.RequestResult }

func (o *observer) RequestStarted(ctx context.Context, m app.RequestMetadata) context.Context { return ctx }
func (o *observer) RequestFinished(ctx context.Context, m app.RequestMetadata, r app.RequestResult) {
	o.result = r
}

func check(label string, got, want any) {
	if fmt.Sprint(got) != fmt.Sprint(want) {
		panic(fmt.Sprintf("%s: got %v want %v", label, got, want))
	}
}

func main() {
	obs := &observer{}
	var denied []string
	mux := http.NewServeMux()
	err := app.RegisterThingsServer(mux, impl{},
		app.WithMiddleware(httpkit.Middleware(httpkit.Config{})),
		app.WithErrorWriter(httpkit.ErrorWriter[*app.ServerError](httpkit.Envelope{})),
		app.WithRequestObserver(obs),
		app.WithAuthorizer(func(ctx context.Context, meta app.RequestMetadata, r *http.Request) error {
			guards, ok := httpkit.ResolveGuards(meta.Guards, r.PathValue)
			if !ok {
				return errors.New("unresolvable guard")
			}
			denied = guards
			if r.Header.Get("Authorization") != "Bearer alice" {
				return errors.New("no")
			}
			httpkit.SetPrincipal(ctx, "alice")
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}

	req := httptest.NewRequest("GET", "/v1/things/42", nil)
	req.Header.Set("Authorization", "Bearer alice")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	check("status", rec.Code, 200)
	check("guards", strings.Join(denied, ","), "things/read/42,audit")
	check("bytes", obs.result.Bytes, len(rec.Body.String()))
	check("path values", obs.result.PathValues["id"], "42")
	check("meta guards", len(obs.result.PathValues), 1)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/things/42", nil))
	check("unauthorized", rec.Code, 401)
	if !strings.Contains(rec.Body.String(), "\"error\"") || !strings.Contains(rec.Body.String(), "request_id") {
		panic("the envelope must carry the request id: " + rec.Body.String())
	}

	req = httptest.NewRequest("GET", "/v1/fail/1", nil)
	req.Header.Set("Authorization", "Bearer alice")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	check("internal", rec.Code, 500)
	if strings.Contains(rec.Body.String(), "secret") {
		panic("cause leaked: " + rec.Body.String())
	}
	fmt.Println("OK")
}
`

func TestGeneratedServerWorksWithHTTPKit(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, httpkitSchema)
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
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/kit\n\ngo 1.27\n\nrequire github.com/1homsi/onekit v0.0.0\n\nreplace github.com/1homsi/onekit => "+root+"\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), httpkitHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
