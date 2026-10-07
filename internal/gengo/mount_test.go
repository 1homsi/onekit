package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const mountSchema = `package app

message Empty {}
message Ping { ok: bool }

service Things {
  base_path: "/v1"
  ping(Empty) -> Ping @get("/ping") @meta("audit.event", "ping")
  boom(Empty) -> Ping @get("/boom")
}
`

const mountHarness = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/1homsi/onekit/httpkit"

	app "example.com/mnt/app"
)

type impl struct{}

func (impl) Ping(ctx context.Context, req *app.Empty) (*app.Ping, error) {
	httpkit.SetFlag(ctx, "skip-audit")
	return &app.Ping{Ok: true}, nil
}

func (impl) Boom(ctx context.Context, req *app.Empty) (*app.Ping, error) { panic("boom") }

type entry struct {
	method, path, event, requestID string
	status                         int
	bytes                          int64
	skipped                        bool
	principal                      any
	panicked                       bool
}

type observer struct{ entries []entry }

func (o *observer) RequestStarted(ctx context.Context, m app.RequestMetadata) context.Context {
	r, _ := app.HTTPRequestFromContext(ctx)
	ctx, _ = httpkit.EnsureState(ctx, r, httpkit.Config{})
	return ctx
}

func (o *observer) RequestFinished(ctx context.Context, m app.RequestMetadata, res app.RequestResult) {
	state := httpkit.StateFrom(ctx)
	o.entries = append(o.entries, entry{
		method: res.Request.Method, path: res.Request.URL.Path, event: m.MetaValue("audit.event"), requestID: state.RequestID,
		status: res.StatusCode, bytes: res.Bytes, skipped: state.Flag("skip-audit"), principal: state.Principal(), panicked: res.Panic != nil,
	})
}

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func main() {
	obs := &observer{}
	mux := http.NewServeMux()
	opts := []app.ServerOption{
		app.WithRequestObserver(obs),
		app.WithMiddleware(httpkit.Middleware(httpkit.Config{})),
		app.WithAuthorizer(func(ctx context.Context, m app.RequestMetadata, r *http.Request) error {
			if r.Header.Get("Authorization") == "" { return errors.New("no") }
			httpkit.SetPrincipal(ctx, "alice")
			return nil
		}),
	}
	if err := app.RegisterThingsServer(mux, append([]any{impl{}}, anyOpts(opts)...)...); err != nil { panic(err) }

	app.Mount(mux, "GET /healthz", httpkit.Health(), app.RequestMetadata{Service: "Ops", Method: "Health", HTTPMethod: "GET", Route: "/healthz", Meta: map[string]string{"audit.event": "health"}}, opts...)
	app.Mount(mux, "GET /raw/{name}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello " + r.PathValue("name")))
	}), app.RequestMetadata{Service: "Ops", Method: "Raw", HTTPMethod: "GET", Route: "/raw/{name}", Meta: map[string]string{"audit.event": "raw"}}, opts...)

	do := func(target string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if auth { req.Header.Set("Authorization", "Bearer x") }
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("/healthz", false); rec.Code != 401 { fail("a mounted handler must go through the authorizer, got %d", rec.Code) }
	rec := do("/raw/bob", true)
	if rec.Code != 200 || rec.Body.String() != "hello bob" { fail("raw: %d %s", rec.Code, rec.Body) }
	last := obs.entries[len(obs.entries)-1]
	if last.event != "raw" || last.status != 200 || last.bytes != 9 || last.principal != "alice" || last.requestID == "" || last.path != "/raw/bob" { fail("mounted route entry: %+v", last) }

	do("/v1/ping", true)
	last = obs.entries[len(obs.entries)-1]
	if last.event != "ping" || !last.skipped || last.method != "GET" { fail("generated route entry: %+v", last) }

	func() {
		defer func() { _ = recover() }()
		do("/v1/boom", true)
	}()
	last = obs.entries[len(obs.entries)-1]
	if !last.panicked || last.status != 500 { fail("a panic must be observed as a 500: %+v", last) }
	fmt.Println("OK")
}

func anyOpts(opts []app.ServerOption) []any {
	out := make([]any, len(opts))
	for i, o := range opts { out[i] = o }
	return out
}
`

func TestMountGivesHandWrittenRoutesTheSameOptionsAndObserverSeesPanics(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, mountSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/mnt\n\ngo 1.27\n\nrequire github.com/1homsi/onekit v0.0.0\n\nreplace github.com/1homsi/onekit => "+root+"\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), mountHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
