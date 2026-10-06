package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sharedRuntimeCheck = `package check

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/shared/gen/onekitrt"
	"example.com/shared/gen/orders"
	"example.com/shared/gen/users"
)

type ordersImpl struct{}

func (ordersImpl) Create(ctx context.Context, req *orders.Order) (*orders.Order, error) { return req, nil }

type usersImpl struct{}

func (usersImpl) Create(ctx context.Context, req *users.User) (*users.User, error) { return req, nil }

func TestOneErrorWriterAuthorizerAndOptionListServeEveryModule(t *testing.T) {
	var _ *orders.ServerError = (*users.ServerError)(nil)
	var _ orders.ErrorWriter = users.ErrorWriter(nil)
	var _ onekitrt.ServerOption = orders.WithMux(nil)

	var seen []string
	opts := []any{
		onekitrt.WithErrorWriter(func(w http.ResponseWriter, r *http.Request, e *onekitrt.ServerError) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(e.Status)
			io.WriteString(w, "{\"error\":{\"code\":\""+e.Code+"\"}}")
		}),
		onekitrt.WithAuthorizer(func(ctx context.Context, m onekitrt.RequestMetadata, r *http.Request) error {
			seen = append(seen, m.Service+"."+m.Method)
			return nil
		}),
		onekitrt.WithRequestID("X-Request-ID"),
	}
	mux := http.NewServeMux()
	if err := orders.RegisterOrdersServer(mux, append([]any{ordersImpl{}}, opts...)...); err != nil {
		t.Fatal(err)
	}
	if err := users.RegisterUsersServer(mux, append([]any{usersImpl{}}, opts...)...); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/v1/orders", "/v1/users"} {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader("{not json"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "\"code\":\"invalid_request_body\"") {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
		if resp.Header.Get("X-Request-ID") == "" {
			t.Fatalf("%s: request id option was not applied", path)
		}
	}
	resp, err := http.Post(srv.URL+"/v1/users", "application/json", strings.NewReader("{\"name\":\"a\"}"))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("valid request: %v %v", err, resp)
	}
	resp.Body.Close()
	if strings.Join(seen, ",") != "Orders.create,Users.create,Users.create" {
		t.Fatalf("the shared authorizer saw %v", seen)
	}
}
`

func TestGoSharedRuntimeLetsOneSetOfHooksServeEveryModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `module = "example.com/shared/gen"

[generate.go-server]
out = "./gen"
runtime = "shared"

[generate.go-client]
out = "./gen"
`)
	writeTestFile(t, filepath.Join(dir, "orders", "orders.onk"), `package orders

message Order { id: string }

service Orders {
  base_path: "/v1"
  create(Order) -> Order @post("/orders")
}
`)
	writeTestFile(t, filepath.Join(dir, "users", "users.onk"), `package users

message User { name: string @len(1, 20) }

service Users {
  base_path: "/v1"
  create(User) -> User @post("/users")
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	gen := filepath.Join(dir, "gen")
	if _, err := os.Stat(filepath.Join(gen, "onekitrt", "runtime.gen.go")); err != nil {
		t.Fatalf("the shared runtime was not written: %v", err)
	}
	for _, pkg := range []string{"orders", "users"} {
		server, err := os.ReadFile(filepath.Join(gen, pkg, "server.gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(server), "type ServerOptions struct") || strings.Contains(string(server), "func (o ServerOptions) WrapHandler") {
			t.Fatalf("%s carries its own server core instead of the shared one", pkg)
		}
	}
	writeTestFile(t, filepath.Join(gen, "go.mod"), "module example.com/shared/gen\n\ngo 1.26\n")
	writeTestFile(t, filepath.Join(gen, "check", "shared_test.go"), sharedRuntimeCheck)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = gen
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shared runtime project failed: %v\n%s", err, out)
	}
}

const sharedRuntimeRichCheck = `package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/shared/gen/onekitrt"
	"example.com/shared/gen/docs"
)

type impl struct{}

func (impl) Delete(ctx context.Context, req *docs.DeleteDoc) (*docs.Ack, error) { return &docs.Ack{Ok: true}, nil }
func (impl) Watch(ctx context.Context, req *docs.WatchDocs, sender docs.SSESender) error {
	return sender.Send(&docs.Doc{Id: "1"})
}
func (impl) Chat(ctx context.Context, req *docs.Frame, out *docs.DocsChatOut) error { return nil }

func TestPrincipalStreamAndSocketsWorkOnTheSharedRuntime(t *testing.T) {
	mux := http.NewServeMux()
	err := docs.RegisterDocsServer(mux, impl{},
		docs.WithPrincipal(func(ctx context.Context, r *http.Request) (*docs.Principal, error) {
			if r.Header.Get("X-Org") == "" {
				return nil, nil
			}
			return &docs.Principal{UserId: "u", Org: r.Header.Get("X-Org")}, nil
		}),
		onekitrt.WithSSEHeartbeat(0),
		docs.WithMaxWSFrameBytes(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	request, _ := http.NewRequest("GET", srv.URL+"/v1/docs/watch?org=acme", nil)
	request.Header.Set("X-Org", "acme")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	denied, _ := http.NewRequest("GET", srv.URL+"/v1/docs/watch?org=acme", nil)
	denied.Header.Set("X-Org", "other")
	resp2, err := http.DefaultClient.Do(denied)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("@authorize through the shared runtime: %d", resp2.StatusCode)
	}
}
`

func TestGoSharedRuntimeCarriesPrincipalStreamsAndSockets(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `module = "example.com/shared/gen"

[generate.go-server]
out = "./gen"
runtime = "shared"
runtime_dir = "rt/core"
`)
	writeTestFile(t, filepath.Join(dir, "docs", "docs.onk"), `package docs

message Principal @principal { user_id: string  org: string }
message DeleteDoc { id: string  owner_org: string }
message WatchDocs { org: string @query }
message Doc { id: string }
message Ack { ok: bool }
message Frame { id: string @ws_id  text: string }

service Docs {
  base_path: "/v1"
  delete(DeleteDoc) -> Ack @post("/docs/delete") @authorize("auth.org == req.owner_org", "no")
  watch(WatchDocs) -> Doc @get("/docs/watch") @stream @authorize("auth.org == req.org", "not your organization")
  chat(Frame) -> Frame @ws("/docs/chat")
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	gen := filepath.Join(dir, "gen")
	if _, err := os.Stat(filepath.Join(gen, "rt", "core", "runtime.gen.go")); err != nil {
		t.Fatalf("runtime_dir was not honored: %v", err)
	}
	runtimeSource, _ := os.ReadFile(filepath.Join(gen, "rt", "core", "runtime.gen.go"))
	if !strings.Contains(string(runtimeSource), "package core") || !strings.Contains(string(runtimeSource), "websocket.AcceptOptions") {
		t.Fatalf("the runtime package must be named after runtime_dir and carry the WebSocket options:\n%.400s", runtimeSource)
	}
	writeTestFile(t, filepath.Join(gen, "go.mod"), "module example.com/shared/gen\n\ngo 1.26\n\nrequire github.com/coder/websocket v1.8.15\n")
	check := strings.ReplaceAll(sharedRuntimeRichCheck, "example.com/shared/gen/onekitrt", "example.com/shared/gen/rt/core")
	check = strings.ReplaceAll(check, "onekitrt.", "core.")
	writeTestFile(t, filepath.Join(gen, "check", "rich_test.go"), check)
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = gen
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = gen
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shared runtime project failed: %v\n%s", err, out)
	}
}

func TestGoRuntimeConfigValidation(t *testing.T) {
	for name, tc := range map[string]struct{ toml, want string }{
		"unknown mode":       {"runtime = \"global\"\n", "runtime must be"},
		"dir without shared": {"runtime_dir = \"rt\"\n", "needs runtime"},
		"dir outside out":    {"runtime = \"shared\"\nruntime_dir = \"../rt\"\n", "inside the output path"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/x\"\n\n[generate.go-server]\nout = \"./gen\"\n"+tc.toml)
			writeTestFile(t, filepath.Join(dir, "a.onk"), "package a\nmessage M { id: string }\nservice S { base_path: \"/v1\"\n  m(M) -> M @post(\"/m\") }\n")
			err := Build(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
