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

const responseControlSchema = `package app

message Empty {}
message Page { etag: string @query  ok: bool }
message Done { ok: bool }
message Login { user: string }
message Session { user: string }

service Web {
  base_path: "/v1"
  files(Page) -> Page @get("/files")
  view(Empty) -> Empty @post("/views") @success(204)
  login(Login) -> Session @post("/login")
  oauth(Empty) -> Empty @get("/oauth/start")
  created(Login) -> Session @post("/things") @success(201)
}
`

const responseControlHarness = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	app "example.com/rc/app"
)

type impl struct{}

func (impl) Files(ctx context.Context, req *app.Page) (*app.Page, error) {
	app.ResponseHeader(ctx).Set("ETag", "\"v1\"")
	app.ResponseHeader(ctx).Set("Cache-Control", "private, max-age=0")
	if req.Etag == "\"v1\"" {
		app.NotModified(ctx)
		return &app.Page{}, nil
	}
	return &app.Page{Ok: true}, nil
}

func (impl) View(ctx context.Context, req *app.Empty) (*app.Empty, error) { return &app.Empty{}, nil }

func (impl) Login(ctx context.Context, req *app.Login) (*app.Session, error) {
	app.SetCookie(ctx, &http.Cookie{Name: "session", Value: "abc", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 3600})
	return &app.Session{User: req.User}, nil
}

func (impl) Oauth(ctx context.Context, req *app.Empty) (*app.Empty, error) {
	app.SetCookie(ctx, &http.Cookie{Name: "state", Value: "s1", Path: "/"})
	app.Redirect(ctx, "https://accounts.example.com/auth?state=s1", http.StatusFound)
	return &app.Empty{}, nil
}

func (impl) Created(ctx context.Context, req *app.Login) (*app.Session, error) {
	return &app.Session{User: req.User}, nil
}

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func do(mux http.Handler, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func main() {
	mux := http.NewServeMux()
	if err := app.RegisterWebServer(mux, impl{}); err != nil { panic(err) }

	rec := do(mux, "GET", "/v1/files", "")
	if rec.Code != 200 || rec.Header().Get("ETag") != "\"v1\"" || !strings.Contains(rec.Body.String(), "true") { fail("200 case: %d %v %s", rec.Code, rec.Header(), rec.Body) }
	rec = do(mux, "GET", "/v1/files?etag=%22v1%22", "")
	if rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("ETag") != "\"v1\"" || rec.Header().Get("Cache-Control") == "" { fail("304 case: %d %v %q", rec.Code, rec.Header(), rec.Body) }

	rec = do(mux, "POST", "/v1/views", "{}")
	if rec.Code != 204 || rec.Body.Len() != 0 { fail("@success(204): %d %q", rec.Code, rec.Body) }

	rec = do(mux, "POST", "/v1/login", "{\"user\":\"u\"}")
	cookie := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"session=abc", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age=3600"} {
		if !strings.Contains(cookie, want) { fail("cookie lacks %s: %s", want, cookie) }
	}
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "\"user\":\"u\"") { fail("login body: %d %s", rec.Code, rec.Body) }

	rec = do(mux, "GET", "/v1/oauth/start", "")
	if rec.Code != 302 || rec.Header().Get("Location") != "https://accounts.example.com/auth?state=s1" || rec.Body.Len() != 0 || !strings.Contains(rec.Header().Get("Set-Cookie"), "state=s1") { fail("redirect: %d %v %q", rec.Code, rec.Header(), rec.Body) }

	rec = do(mux, "POST", "/v1/things", "{\"user\":\"x\"}")
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), "\"user\":\"x\"") { fail("@success(201): %d %s", rec.Code, rec.Body) }
	fmt.Println("OK")
}
`

func TestGoResponseControlCookiesRedirectsAndBodylessStatuses(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onklang.Parse(responseControlSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server"}})
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
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/rc\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), responseControlHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSuccessDecoratorValidation(t *testing.T) {
	for _, tc := range []struct{ rpc, want string }{
		{`@success(204)`, ""},
		{`@success(404)`, "2xx"},
		{`@success`, "one status"},
		{`@success(abc)`, "2xx"},
	} {
		ast, err := onklang.Parse("package app\nmessage E {}\nservice S { a(E) -> E @post(\"/x\") " + tc.rpc + " }\n")
		if err != nil {
			t.Fatal(err)
		}
		_, err = onkcompile.Compile([]onkcompile.Source{{Path: "a.onk", AST: ast}})
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: got %v want %q", tc.rpc, err, tc.want)
		}
	}
}
