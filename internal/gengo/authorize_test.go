package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const authorizeGoSchema = `package app

message Principal @principal {
  user_id: string
  roles: string[]
  org: string
}

message DeleteDoc {
  id: string
  owner_org: string
}

message WatchDocs {
  org: string
}

message Doc { id: string }
message Ack { ok: bool }

service Docs {
  base_path: "/v1"
  delete(DeleteDoc) -> Ack @post("/docs/delete")
    @requires("docs:write")
    @authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
    @authorize("size(auth.user_id) > 0", "sign in first")
  open(DeleteDoc) -> Ack @post("/docs/open")
  watch(WatchDocs) -> Doc @get("/docs/watch") @stream
    @authorize("auth.org == req.org", "not your organization")
}
`

const authorizeGoHarness = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	app "example.com/authz/app"
)

type impl struct{}

func (impl) Delete(ctx context.Context, req *app.DeleteDoc) (*app.Ack, error) {
	principal, ok := app.PrincipalFromContext(ctx)
	if !ok || principal.UserId == "" {
		return nil, errors.New("handler should see the principal")
	}
	return &app.Ack{Ok: true}, nil
}

func (impl) Open(ctx context.Context, req *app.DeleteDoc) (*app.Ack, error) {
	return &app.Ack{Ok: true}, nil
}

func (impl) Watch(ctx context.Context, req *app.WatchDocs, out app.SSESender) error {
	return out.Send(&app.Doc{Id: "d1"})
}

type statusError struct{ code int }

func (e statusError) Error() string       { return "denied" }
func (e statusError) HTTPStatusCode() int { return e.code }

func post(url, who, body string) (int, map[string]any) {
	request, _ := http.NewRequest("POST", url, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-User", who)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}

func check(name string, got, want int) {
	if got != want {
		panic(fmt.Sprintf("%s: status %d, want %d", name, got, want))
	}
}

func main() {
	resolve := func(ctx context.Context, r *http.Request) (*app.Principal, error) {
		switch r.Header.Get("X-User") {
		case "admin":
			return &app.Principal{UserId: "u1", Roles: []string{"admin"}, Org: "acme"}, nil
		case "member":
			return &app.Principal{UserId: "u2", Org: "acme"}, nil
		case "anonymous":
			return &app.Principal{}, nil
		case "teapot":
			return nil, statusError{code: 418}
		}
		return nil, errors.New("unknown caller")
	}

	mux := http.NewServeMux()
	if err := app.RegisterDocsServer(mux, impl{}, app.WithScopes(func(ctx context.Context, r *http.Request) ([]string, error) { return []string{"docs:write"}, nil }), app.WithPrincipal(resolve)); err != nil {
		panic(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	status, _ := post(server.URL+"/v1/docs/delete", "admin", ` + "`" + `{"id":"1","owner_org":"other"}` + "`" + `)
	check("admin may delete anything", status, 200)
	status, _ = post(server.URL+"/v1/docs/delete", "member", ` + "`" + `{"id":"1","owner_org":"acme"}` + "`" + `)
	check("member may delete in their org", status, 200)

	status, body := post(server.URL+"/v1/docs/delete", "member", ` + "`" + `{"id":"1","owner_org":"other"}` + "`" + `)
	check("member may not delete elsewhere", status, 403)
	if body["message"] != "not allowed to delete this document" {
		panic(fmt.Sprintf("message: %v", body))
	}
	status, body = post(server.URL+"/v1/docs/delete", "anonymous", ` + "`" + `{"id":"1","owner_org":"acme"}` + "`" + `)
	check("every failed rule is listed", status, 403)
	if violations, _ := body["violations"].([]any); len(violations) != 2 {
		panic(fmt.Sprintf("violations: %v", body))
	}
	status, _ = post(server.URL+"/v1/docs/delete", "stranger", ` + "`" + `{"id":"1"}` + "`" + `)
	check("unknown caller is unauthorized", status, 401)
	status, _ = post(server.URL+"/v1/docs/delete", "teapot", ` + "`" + `{"id":"1"}` + "`" + `)
	check("the resolver controls the status", status, 418)
	status, _ = post(server.URL+"/v1/docs/open", "stranger", ` + "`" + `{"id":"1"}` + "`" + `)
	check("methods without @authorize never ask who the caller is", status, 200)

	response, err := http.Get(server.URL + "/v1/docs/watch?org=acme")
	if err != nil {
		panic(err)
	}
	response.Body.Close()
	check("stream without a caller is unauthorized", response.StatusCode, 401)

	bare := http.NewServeMux()
	if err := app.RegisterDocsServer(bare, impl{}); err != nil {
		panic(err)
	}
	bareServer := httptest.NewServer(bare)
	defer bareServer.Close()
	status, _ = post(bareServer.URL+"/v1/docs/delete", "admin", ` + "`" + `{"id":"1"}` + "`" + `)
	check("a missing principal provider fails closed", status, 500)
	fmt.Println("OK")
}
`

func TestGeneratedGoServerEnforcesAuthorize(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, authorizeGoSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/authz\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), authorizeGoHarness)
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("generated code does not vet: %v\n%s", err, out)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("generated server misbehaved: %v\n%s", err, out)
	}
}
