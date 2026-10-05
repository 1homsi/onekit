package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const metaSchema = `package app

message Doc { slug: string }
message Done { ok: bool }

service Docs {
  base_path: "/v1"
  edit(Doc) -> Done @post("/apps/{slug}/edit") @meta("guard", "app/edit/:slug") @meta("audit.event", "app.update")
  view(Doc) -> Done @get("/apps/{slug}") @meta("guard", "app/use/:slug")
  health(Doc) -> Done @get("/health/{slug}")
}
`

const metaHarness = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"

	app "example.com/meta/app"
)

type impl struct{}

func (impl) Edit(ctx context.Context, req *app.Doc) (*app.Done, error) {
	metadata, _ := app.RequestMetadataFromContext(ctx)
	if metadata.MetaValue("audit.event") != "app.update" {
		panic("the handler must see the route metadata")
	}
	return &app.Done{Ok: true}, nil
}
func (impl) View(ctx context.Context, req *app.Doc) (*app.Done, error) { return &app.Done{Ok: true}, nil }
func (impl) Health(ctx context.Context, req *app.Doc) (*app.Done, error) { return &app.Done{Ok: true}, nil }

type denied struct{}

func (denied) Error() string       { return "denied" }
func (denied) HTTPStatusCode() int { return http.StatusForbidden }

func main() {
	var mu sync.Mutex
	var audited []string
	guard := func(ctx context.Context, metadata app.RequestMetadata, r *http.Request) error {
		rule := metadata.MetaValue("guard")
		if rule == "" {
			return nil
		}
		slug := r.PathValue("slug")
		if strings.HasPrefix(rule, "app/edit/") && slug != "mine" {
			return denied{}
		}
		return nil
	}
	auditing := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if metadata, ok := app.RequestMetadataFromContext(r.Context()); ok && metadata.MetaValue("audit.event") != "" {
				mu.Lock()
				audited = append(audited, metadata.MetaValue("audit.event")+" "+r.PathValue("slug"))
				mu.Unlock()
			}
		})
	}
	mux := http.NewServeMux()
	if err := app.RegisterDocsServer(mux, impl{}, app.WithAuthorizer(guard), app.WithMiddleware(auditing)); err != nil {
		panic(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	call := func(method, path string) int {
		request, _ := http.NewRequest(method, server.URL+path, strings.NewReader("{}"))
		response, err := http.DefaultClient.Do(request)
		if err != nil { panic(err) }
		response.Body.Close()
		return response.StatusCode
	}
	for name, want := range map[string]int{
		"POST /v1/apps/mine/edit": 200, "POST /v1/apps/other/edit": 403, "GET /v1/apps/other": 200, "GET /v1/health/x": 200,
	} {
		var method, path string
		fmt.Sscanf(name, "%s %s", &method, &path)
		if got := call(method, path); got != want {
			panic(fmt.Sprintf("%s: %d, want %d", name, got, want))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(audited)
	if fmt.Sprint(audited) != "[app.update mine app.update other]" {
		panic(fmt.Sprintf("denied attempts are audited too, since middleware runs outside the authorizer: %v", audited))
	}
	fmt.Println("OK")
}
`

func TestGoRouteMetadataReachesAuthorizersMiddlewareAndHandlers(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, metaSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/meta\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), metaHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
