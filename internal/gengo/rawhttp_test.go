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

const rawHTTPSchema = `package app

message Empty {}
message Bundle { slug: string }
message Upload { slug: string  key: string }
message Info { ok: bool }

service Apps {
  base_path: "/apps"
  bundle(Bundle) -> Empty @get("/{slug}/bundle.js") @http("application/javascript") @guard("apps/read/:slug") @meta("audit.event", "bundle")
  put_object(Upload) -> Empty @put("/{slug}/objects/{key...}") @http @max_body("1KiB")
  info(Bundle) -> Info @get("/{slug}/info")
}
`

const rawHTTPHarness = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/1homsi/onekit/httpkit"

	app "example.com/raw/app"
)

type impl struct{}

func (impl) Bundle(w http.ResponseWriter, r *http.Request, req *app.Bundle) error {
	if req.Slug == "missing" { return &statusError{404, "no such app"} }
	if req.Slug == "late" { w.WriteHeader(202); return errors.New("too late") }
	body := []byte("console.log('" + req.Slug + "')")
	tag := httpkit.ETag(body)
	if httpkit.IfNoneMatch(r, tag) {
		w.Header().Set("ETag", tag)
		app.NotModified(r.Context())
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("ETag", tag)
	w.Header().Set("X-Detool-Slot", "1")
	_, err := w.Write(body)
	return err
}

func (impl) PutObject(w http.ResponseWriter, r *http.Request, req *app.Upload) error {
	data, err := io.ReadAll(r.Body)
	if err != nil { return err }
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s/%s=%d:%s", req.Slug, req.Key, len(data), r.Header.Get("Content-Type"))
	return nil
}

func (impl) Info(ctx context.Context, req *app.Bundle) (*app.Info, error) { return &app.Info{Ok: true}, nil }

type statusError struct { code int; msg string }
func (e *statusError) Error() string { return e.msg }
func (e *statusError) HTTPStatusCode() int { return e.code }
func (e *statusError) PublicMessage() string { return e.msg }

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func main() {
	var seenGuards []string
	mux := http.NewServeMux()
	err := app.RegisterAppsServer(mux, impl{},
		app.WithErrorWriter(httpkit.ErrorWriter[*app.ServerError](httpkit.Envelope{})),
		app.WithAuthorizer(func(ctx context.Context, m app.RequestMetadata, r *http.Request) error {
			if m.Method == "bundle" {
				seenGuards, _ = httpkit.ResolveGuards(m.Guards, r.PathValue)
			}
			return nil
		}),
	)
	if err != nil { panic(err) }

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apps/blog/bundle.js", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/javascript" || rec.Header().Get("X-Detool-Slot") != "1" || rec.Body.String() != "console.log('blog')" { fail("bundle: %d %v %s", rec.Code, rec.Header(), rec.Body) }
	if strings.Join(seenGuards, ",") != "apps/read/blog" { fail("guards: %v", seenGuards) }
	etag := rec.Header().Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/apps/blog/bundle.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 304 || rec.Body.Len() != 0 { fail("304: %d %q", rec.Code, rec.Body) }

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apps/missing/bundle.js", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "\"error\"") || !strings.Contains(rec.Body.String(), "no such app") { fail("a returned error uses the envelope: %d %s", rec.Code, rec.Body) }

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apps/late/bundle.js", nil))
	if rec.Code != 202 || strings.Contains(rec.Body.String(), "error") { fail("an error after the response started must not be written: %d %s", rec.Code, rec.Body) }

	up := httptest.NewRequest(http.MethodPut, "/apps/blog/objects/a/b/c.png", strings.NewReader("12345"))
	up.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, up)
	if rec.Code != 200 || rec.Body.String() != "blog/a/b/c.png=5:image/png" { fail("upload: %d %q", rec.Code, rec.Body) }

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/apps/blog/objects/big", strings.NewReader(strings.Repeat("x", 4096))))
	if rec.Code < 400 { fail("the @max_body limit must apply to a raw upload, got %d %q", rec.Code, rec.Body) }

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apps/blog/info", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "true") { fail("a normal route next to raw ones: %d %s", rec.Code, rec.Body) }
	fmt.Println("OK")
}
`

func TestGoRawHTTPRoutes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(rawHTTPSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server", "go-client", "ts-client", "openapi"}})
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
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/raw\n\ngo 1.27\n\nrequire github.com/1homsi/onekit v0.0.0\n\nreplace github.com/1homsi/onekit => "+root+"\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), rawHTTPHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRawHTTPContractRules(t *testing.T) {
	cases := []struct {
		name, schema string
		opts         onkcompile.CompileOptions
		want         string
	}{
		{"unbound field", `message R { a: string  b: string }
message E {}
service S { x(R) -> E @put("/x/{a}") @http }`, onkcompile.CompileOptions{Targets: []string{"go-server"}}, `field "b"`},
		{"python target", `message R { a: string }
message E {}
service S { x(R) -> E @get("/x/{a}") @http }`, onkcompile.CompileOptions{Targets: []string{"go-server", "python-client"}}, "go-server, go-client, ts-client and openapi targets only"},
		{"with stream", `message R { a: string }
message E {}
service S { x(R) -> E @get("/x/{a}") @http @stream }`, onkcompile.CompileOptions{Targets: []string{"go-server"}}, "cannot be combined"},
		{"ok", `message R { a: string  q: string @query }
message E {}
service S { x(R) -> E @get("/x/{a}") @http("text/plain") }`, onkcompile.CompileOptions{Targets: []string{"go-server", "ts-client", "go-client", "openapi"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := onklang.Parse("package app\n" + tc.schema + "\n")
			if err != nil {
				t.Fatal(err)
			}
			_, err = onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "a.onk", AST: ast}}, tc.opts)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v want %q", err, tc.want)
			}
		})
	}
}

func TestGoRawHTTPClient(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(rawHTTPSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server", "go-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	types, _ := GenerateTypesWithResolver(file, nil)
	validation, _ := GenerateValidationWithResolver(file, nil)
	client, err := GenerateClientWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/rawc\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "client.go"), string(client))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	app "example.com/rawc/app"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("ETag", "\"abc\"")
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprintf(w, "%s %s %s %s", r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), data)
	}))
	defer srv.Close()
	c := app.NewAppsClient(srv.URL)
	resp, err := c.Bundle(context.Background(), &app.Bundle{Slug: "blog"})
	if err != nil { panic(err) }
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 418 || resp.Header.Get("ETag") != "\"abc\"" || string(body) != "GET /apps/blog/bundle.js  " { fmt.Println("get:", resp.StatusCode, resp.Header, string(body)); os.Exit(1) }

	resp, err = c.PutObject(context.Background(), &app.Upload{Slug: "blog", Key: "a/b.png"}, strings.NewReader("bytes"), "image/png")
	if err != nil { panic(err) }
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "PUT /apps/blog/objects/a/b.png image/png bytes" { fmt.Println("put:", string(body)); os.Exit(1) }
	fmt.Println("OK")
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
