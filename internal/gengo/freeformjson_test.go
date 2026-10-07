package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const freeFormSchema = `package app

message RunIn { id: string  input: json @object }
message RunOut { value: json @unwrap }
message Raw { id: string  payload: json }

service Queries {
  base_path: "/v1"
  run(RunIn) -> RunOut @post("/queries/{id}/run") @body("input")
  any(Raw) -> RunOut @post("/any/{id}") @body("payload")
}
`

const freeFormHarness = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	app "example.com/ff/app"
)

type impl struct{ got []byte }

func (s *impl) Run(ctx context.Context, req *app.RunIn) (*app.RunOut, error) {
	s.got = req.Input
	switch req.Id {
	case "null": return &app.RunOut{Value: json.RawMessage("null")}, nil
	case "nil": return &app.RunOut{}, nil
	case "array": return &app.RunOut{Value: json.RawMessage("[3,1,2]")}, nil
	case "string": return &app.RunOut{Value: json.RawMessage("\"hi\"")}, nil
	}
	return &app.RunOut{Value: json.RawMessage("{\"z\":1,\"a\":2}")}, nil
}

func (s *impl) Any(ctx context.Context, req *app.Raw) (*app.RunOut, error) {
	s.got = req.Payload
	return &app.RunOut{Value: req.Payload}, nil
}

func fail(format string, args ...any) { fmt.Printf(format+"\n", args...); os.Exit(1) }

func call(mux http.Handler, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, strings.NewReader(body)))
	return rec
}

func main() {
	srv := &impl{}
	mux := http.NewServeMux()
	if err := app.RegisterQueriesServer(mux, srv); err != nil { panic(err) }

	for id, want := range map[string]string{"null": "null", "nil": "null", "array": "[3,1,2]", "string": "\"hi\"", "obj": "{\"z\":1,\"a\":2}"} {
		rec := call(mux, "/v1/queries/"+id+"/run", "{\"b\":1,\"a\":{\"y\":2,\"x\":3}}")
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != want { fail("response %s: %d %q want %q", id, rec.Code, rec.Body, want) }
	}
	call(mux, "/v1/queries/obj/run", "{\"b\":1,\"a\":{\"y\":2,\"x\":3}}")
	if string(srv.got) != "{\"b\":1,\"a\":{\"y\":2,\"x\":3}}" { fail("the request object must reach the handler untouched, got %s", srv.got) }

	if rec := call(mux, "/v1/queries/obj/run", "[1,2]"); rec.Code != 400 { fail("@object must reject an array body, got %d", rec.Code) }

	for _, body := range []string{"null", "[1,[2]]", "\"text\"", "12.50", "true"} {
		rec := call(mux, "/v1/any/1", body)
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != body { fail("free-form %s: %d %q", body, rec.Code, rec.Body) }
		if string(srv.got) != body { fail("handler saw %q for %q", srv.got, body) }
	}
	fmt.Println("OK")
}
`

func TestGoFreeFormJSONBodiesPassThroughUntouched(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, freeFormSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/ff\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), freeFormHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
