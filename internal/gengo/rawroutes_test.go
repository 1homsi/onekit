package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

const rawRoutesSchema = `namespace app;

model Empty {}

model Slug {
  slug: string;
}

model DraftBody {
  source: string;
  minify: boolean;
}

model DraftRequest {
  slug: string;
  @body
  draft: DraftBody;
}

model Policy {
  allowed: boolean;
  reason: string;
}

model UploadResult {
  name: string;
  size: int32;
}

model ProxyRequest {
  slug: string;
  resource: string;
  path: string;
  @query
  trace: string;
}

@route("/apps")
interface Apps {
  @get
  @route("/{slug}/bundle.js")
  @Onkt.http(#{ contentType: "application/javascript" })
  @Onkt.guard("app/use/:slug")
  @Onkt.meta("audit.event", "bundle")
  bundle(...Slug): Empty;

  @post
  @route("/{slug}/bundle-draft.js")
  @Onkt.http(#{ contentType: "application/javascript", request: "json" })
  draft(...DraftRequest): Empty;

  @get
  @route("/{slug}/local-execution")
  @Onkt.http(#{ response: "json" })
  policy(...Slug): Policy;

  @get
  @route("/{slug}/local-execution/run")
  @Onkt.http(#{ response: "websocket" })
  run(...Slug): Empty;

  @get
  @route("/{slug}/oauth")
  @Onkt.http(#{ response: "redirect" })
  oauth(...Slug): Empty;

  @post
  @route("/{slug}/uploads")
  @Onkt.http(#{ request: "multipart", response: "json" })
  @Onkt.maxBody("64MiB")
  upload(...Slug): UploadResult;

  @Onkt.method("ANY")
  @route("/{slug}/proxy/{resource}/{+path}")
  @Onkt.http
  @Onkt.guard("app/use/:slug")
  proxy(...ProxyRequest): Empty;

  @get
  @route("/{slug}/info")
  info(...Slug): Policy;

  @get
  @route("/{slug}/hints")
  @Onkt.http
  hints(...Slug): Empty;
}
`

const rawRoutesHarness = `package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	app "example.com/rawroutes/app"
)

type impl struct{}

func (impl) Bundle(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	fmt.Fprintf(w, "bundle %s", req.Slug)
	return nil
}

func (impl) Draft(w http.ResponseWriter, r *http.Request, req *app.DraftRequest) error {
	if req.Draft != nil {
		return errors.New("the JSON body is the handler's to read, not bound by the server")
	}
	data, _ := io.ReadAll(r.Body)
	w.Header().Set("X-Content-Type", r.Header.Get("Content-Type"))
	_, err := w.Write(data)
	return err
}

func (impl) Policy(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write([]byte("{\"allowed\":true,\"reason\":\"" + req.Slug + "\"}"))
	return err
}

func (impl) Run(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	if _, ok := w.(http.Flusher); !ok {
		return errors.New("the response writer hides http.Flusher")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("the response writer hides http.Hijacker")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return err
	}
	defer conn.Close()
	rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\nhello " + req.Slug)
	return rw.Flush()
}

func (impl) Oauth(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	app.Redirect(r.Context(), "https://example.com/auth?slug="+req.Slug, http.StatusFound)
	http.Redirect(w, r, "https://example.com/auth?slug="+req.Slug, http.StatusFound)
	return nil
}

func (impl) Upload(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return err
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"name\":%q,\"size\":%d}", header.Filename, header.Size)
	return nil
}

func (impl) Info(ctx context.Context, req *app.Slug) (*app.Policy, error) {
	order = append(order, "info")
	return &app.Policy{Allowed: true}, nil
}

var order []string

func (impl) Hints(w http.ResponseWriter, r *http.Request, req *app.Slug) error {
	w.Header().Set("Link", "</style.css>; rel=preload")
	w.WriteHeader(http.StatusEarlyHints)
	http.Error(w, "gone", http.StatusNotFound)
	return nil
}

func (impl) Proxy(w http.ResponseWriter, r *http.Request, req *app.ProxyRequest) error {
	order = append(order, "handler")
	fmt.Fprintf(w, "%s %s|%s|%s|%s|%s|%s", r.Method, r.URL.Path, req.Slug, req.Resource, req.Path, req.Trace, r.PathValue("path"))
	return nil
}

type denied struct{}

func (*denied) Error() string       { return "denied" }
func (*denied) HTTPStatusCode() int { return http.StatusForbidden }

type statusObserver struct{ seen *[]int }

func (statusObserver) RequestStarted(ctx context.Context, m app.RequestMetadata) context.Context {
	return ctx
}

func (o statusObserver) RequestFinished(ctx context.Context, m app.RequestMetadata, result app.RequestResult) {
	if m.Method == "hints" {
		*o.seen = append(*o.seen, result.StatusCode)
	}
}

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func main() {
	var methods, routes []string
	var observed []int
	mux := http.NewServeMux()
	err := app.RegisterAppsServer(mux, impl{},
		app.WithRequestObserver(statusObserver{&observed}),
		app.WithAuthorizer(func(ctx context.Context, m app.RequestMetadata, r *http.Request) error {
			order = append(order, "authorizer")
			if strings.Contains(r.URL.Path, "forbidden") {
				return &denied{}
			}
			methods = append(methods, m.HTTPMethod)
			routes = append(routes, m.Route)
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}
	srv := httptest.NewServer(app.PreserveSegments(mux))
	defer srv.Close()

	get := func(method, path string, body io.Reader, header http.Header) (int, string, http.Header) {
		req, err := http.NewRequest(method, srv.URL+path, body)
		if err != nil {
			panic(err)
		}
		for k, v := range header {
			req.Header[k] = v
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data), resp.Header
	}

	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		code, body, _ := get(method, "/apps/blog/proxy/pg/a/b?trace=t1", nil, nil)
		want := method + " /apps/blog/proxy/pg/a/b|blog|pg|a/b|t1|a/b"
		if code != 200 || body != want {
			fail("any %s: %d %q want %q", method, code, body, want)
		}
	}
	if !strings.Contains(strings.Join(methods, ","), "ANY") || routes[len(routes)-1] != "/apps/{slug}/proxy/{resource}/{path...}" {
		fail("metadata: %v %v", methods, routes)
	}

	code, body, _ := get("GET", "/apps/blog/proxy/pg/a//b/./c/../d", nil, nil)
	want := "GET /apps/blog/proxy/pg/a//b/./c/../d|blog|pg|a//b/./c/../d||a//b/./c/../d"
	if code != 200 || body != want {
		fail("segments are not preserved: %d %q want %q", code, body, want)
	}

	order = nil
	code, body, _ = get("GET", "/apps/blog/proxy/pg/../../secret/x", nil, nil)
	want = "GET /apps/blog/proxy/pg/../../secret/x|blog|pg|../../secret/x||../../secret/x"
	if code != 200 || body != want {
		fail("a .. tail must reach the handler untouched: %d %q want %q", code, body, want)
	}
	if strings.Join(order, ",") != "authorizer,handler" {
		fail("the authorizer must run before the handler: %v", order)
	}

	order = nil
	code, _, _ = get("GET", "/apps/blog/proxy/forbidden/../x", nil, nil)
	if code != http.StatusForbidden || strings.Join(order, ",") != "authorizer" {
		fail("a denied request with a .. tail must not reach the handler: %d %v", code, order)
	}

	code, body, _ = get("GET", "/apps/blog/hints", nil, nil)
	if code != http.StatusNotFound || !strings.Contains(body, "gone") || len(observed) != 1 || observed[0] != http.StatusNotFound {
		fail("a 103 must not hide the final status: client %d %q, observer %v", code, body, observed)
	}

	plainTyped, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(srv.URL + "/apps/blog//info")
	if err != nil {
		panic(err)
	}
	plainTyped.Body.Close()
	if plainTyped.StatusCode < 300 || plainTyped.StatusCode > 399 || !strings.HasSuffix(plainTyped.Header.Get("Location"), "/apps/blog/info") {
		fail("a typed route keeps the mux's path cleaning: %d %q", plainTyped.StatusCode, plainTyped.Header.Get("Location"))
	}

	plain := httptest.NewServer(mux)
	defer plain.Close()
	cleaned := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cleaned.Get(plain.URL + "/apps/blog/proxy/pg/a//b")
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 || !strings.HasSuffix(resp.Header.Get("Location"), "/apps/blog/proxy/pg/a/b") {
		fail("without PreserveSegments the mux cleans the path with a redirect, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	code, body, _ = get("POST", "/apps/blog/bundle-draft.js", strings.NewReader("{\"source\":\"x\"}"), http.Header{"Content-Type": {"application/json"}})
	if code != 200 || body != "{\"source\":\"x\"}" {
		fail("draft: %d %q", code, body)
	}

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(conn, "GET /apps/blog/local-execution/run HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
	raw, _ := io.ReadAll(bufio.NewReader(conn))
	conn.Close()
	if !strings.Contains(string(raw), "101 Switching Protocols") || !strings.HasSuffix(string(raw), "hello blog") {
		fail("hijack: %q", raw)
	}

	code, _, header := get("GET", "/apps/blog/oauth", nil, nil)
	if code != http.StatusFound || header.Get("Location") != "https://example.com/auth?slug=blog" {
		fail("redirect: %d %v", code, header)
	}

	client := app.NewAppsClient(srv.URL)
	policy, err := client.Policy(context.Background(), &app.Slug{Slug: "blog"})
	if err != nil || !policy.Allowed || policy.Reason != "blog" {
		fail("typed json response: %+v %v", policy, err)
	}
	resp2, err := client.Draft(context.Background(), &app.DraftRequest{Slug: "blog", Draft: &app.DraftBody{Source: "let a", Minify: true}})
	if err != nil {
		panic(err)
	}
	data, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.Header.Get("X-Content-Type") != "application/json" || !strings.Contains(string(data), "let a") || !strings.Contains(string(data), "\"minify\":true") {
		fail("typed json request: %v %q", resp2.Header, data)
	}
	resp3, err := client.Proxy(context.Background(), &app.ProxyRequest{Slug: "blog", Resource: "pg", Path: "a/b", Trace: "t"}, "PATCH", nil, "")
	if err != nil {
		panic(err)
	}
	data, _ = io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if string(data) != "PATCH /apps/blog/proxy/pg/a/b|blog|pg|a/b|t|a/b" {
		fail("client with any method: %q", data)
	}
	for _, tc := range []struct {
		name string
		url  func() (string, error)
		want string
	}{
		{"plain", func() (string, error) { return client.BundleURL(&app.Slug{Slug: "a b"}) }, srv.URL + "/apps/a%20b/bundle.js"},
		{"wildcard and query", func() (string, error) {
			return client.ProxyURL(&app.ProxyRequest{Slug: "blog", Resource: "pg", Path: "a/b c", Trace: "t 1"})
		}, srv.URL + "/apps/blog/proxy/pg/a/b%20c?trace=t+1"},
		{"websocket", func() (string, error) { return client.RunURL(&app.Slug{Slug: "blog"}) }, srv.URL + "/apps/blog/local-execution/run"},
		{"redirect", func() (string, error) { return client.OauthURL(&app.Slug{Slug: "blog"}) }, srv.URL + "/apps/blog/oauth"},
	} {
		if got, err := tc.url(); err != nil || got != tc.want {
			fail("url builder %s: %q %v want %q", tc.name, got, err, tc.want)
		}
	}
	if got, err := client.ProxyURL(&app.ProxyRequest{Slug: "blog", Resource: "pg", Path: "a/../b"}); err == nil || got != "" {
		fail("a .. segment in a wildcard tail must be rejected: %q %v", got, err)
	}
	fmt.Println("OK")
}
`

type rawRoutesOutput struct{ types, validation, server, client string }

func compileRawRoutes(t *testing.T) rawRoutesOutput {
	t.Helper()
	ast, err := onktest.Parse(rawRoutesSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server", "go-client", "ts-client", "ts-server", "openapi"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	typesOut, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	validationOut, err := GenerateValidationWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverOut, err := GenerateServerWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientOut, err := GenerateClientWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rawRoutesOutput{string(typesOut), string(validationOut), string(serverOut), string(clientOut)}
}

func TestGoRawRoutesRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	generated := compileRawRoutes(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/rawroutes\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), generated.types)
	writeFile(t, filepath.Join(dir, "app", "validate.go"), generated.validation)
	writeFile(t, filepath.Join(dir, "app", "server.go"), generated.server)
	writeFile(t, filepath.Join(dir, "app", "client.go"), generated.client)
	writeFile(t, filepath.Join(dir, "main.go"), rawRoutesHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSharedRuntimeCompilesRawRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onktest.Parse(rawRoutesSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"go-server"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	types, _ := GenerateTypesWithResolver(file, nil)
	validation, _ := GenerateValidationWithResolver(file, nil)
	server, err := GenerateServerWithOptions(file, nil, Options{SharedRuntime: &SharedRuntime{ImportPath: "example.com/shared/rt", Package: "rt"}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := GenerateServerRuntime("rt", false, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shared\n\ngo 1.27\n")
	writeTreeFile(t, filepath.Join(dir, "rt", "rt.go"), string(runtime))
	writeTreeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeTreeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeTreeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRawRoutesCompileRules(t *testing.T) {
	cases := []struct {
		name, schema string
		want         string
	}{
		{"any without http", `
model R {
  a: string;
}

model E {}

interface S {
  @Onkt.method("ANY")
  @route("/x/{a}")
  x(...R): E;
}
`, "@any needs @http"},
		{"json request without body", `
model R {
  a: string;
}

model E {}

interface S {
  @post
  @route("/x/{a}")
  @Onkt.http(#{ request: "json" })
  x(...R): E;
}
`, "needs @body"},
		{"body without json request", `
model B {
  v: string;
}

model R {
  a: string;
  @body
  b: B;
}

model E {}

interface S {
  @post
  @route("/x/{a}")
  @Onkt.http
  x(...R): E;
}
`, `needs @http(request: "json")`},
		{"request on a get", `
model R {
  a: string;
}

model E {}

interface S {
  @get
  @route("/x/{a}")
  @Onkt.http(#{ request: "multipart" })
  x(...R): E;
}
`, "needs a verb with a body"},
		{"typed response without json", `
model R {
  a: string;
}

model E {
  v: string;
}

interface S {
  @get
  @route("/x/{a}")
  @Onkt.http
  x(...R): E;
}
`, "handler writes the response itself"},
		{"typed response with json", `
model R {
  a: string;
}

model E {
  v: string;
}

interface S {
  @get
  @route("/x/{a}")
  @Onkt.http(#{ response: "json" })
  x(...R): E;
}
`, ""},
		{"every target", `
model R {
  a: string;
}

model E {}

interface S {
  @Onkt.method("ANY")
  @route("/x/{+a}")
  @Onkt.http
  x(...R): E;
}
`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := onktest.Parse(`namespace app;
` + tc.schema + "\n")
			if err != nil {
				t.Fatal(err)
			}
			_, err = onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "a.tsp", AST: ast}}, onkcompile.CompileOptions{})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v want %q", err, tc.want)
			}
		})
	}
}

// TypeSpec's typed @Onkt.http options refuse these values; documents from
// any frontend that carry them are refused too.
func TestRawRoutesRejectUnknownHTTPOptionsInDocuments(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	cases := []struct {
		name string
		verb string
		http onkschema.Decorator
		want string
	}{
		{"unknown request kind", "post", dec("http", onktest.Named("request", q("xml"))), "@http request must be one of"},
		{"unknown response kind", "get", dec("http", onktest.Named("response", q("sse"))), "@http response must be one of"},
		{"unknown argument", "get", dec("http", onktest.Named("mode", q("x"))), `no argument "mode"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := onktest.File("app",
				onktest.Message("R", onktest.Field("a", "string")),
				onktest.Message("E"),
				onktest.Service("S", "", onktest.RPC("x", "R", "E", dec(tc.verb, q("/x/{a}")), tc.http)),
			)
			_, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "a.tsp", AST: file}}, onkcompile.CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %q", err, tc.want)
			}
		})
	}
}
