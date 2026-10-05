package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const errorWriterSchema = `package app

message ErrorBody { code: string message: string request_id: string }
message ApiFailure @status(409) { error: ErrorBody }

message Thing {
  id: int64
  page: int32 @query
}
message Draft {
  id: int64
  name: string @len(2, 10)
}
message Done { ok: bool }

service Things {
  base_path: "/v1"
  headers: { "X-Tenant": string @required }
  get(Thing) -> Done | ApiFailure @get("/things/{id}")
  save(Draft) -> Done @post("/things/save")
}
`

const errorWriterHarness = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	app "example.com/errs/app"
)

type impl struct{}

func (impl) Get(ctx context.Context, req *app.Thing) (*app.Done, error) {
	switch req.Id {
	case 1:
		return &app.Done{Ok: true}, nil
	case 2:
		return nil, &app.ApiFailure{Error_: &app.ErrorBody{Code: "conflict", Message: "taken", RequestId: "typed"}}
	}
	return nil, errors.New("database exploded: secret detail")
}

func (impl) Save(ctx context.Context, req *app.Draft) (*app.Done, error) { return &app.Done{Ok: true}, nil }

func do(handler http.Handler, method, path, body string, headers map[string]string) (int, map[string]any) {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

func envelope(w http.ResponseWriter, r *http.Request, e *app.ServerError) {
	id, _ := app.RequestIDFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": e.Code, "message": e.Message, "request_id": id, "field": e.Field, "violations": e.Violations}})
}

func check(name string, got, want any) {
	if fmt.Sprint(got) != fmt.Sprint(want) {
		panic(fmt.Sprintf("%s: got %v, want %v", name, got, want))
	}
}

func errorOf(body map[string]any) map[string]any {
	inner, _ := body["error"].(map[string]any)
	return inner
}

func main() {
	tenant := map[string]string{"X-Tenant": "t1"}

	custom := http.NewServeMux()
	if err := app.RegisterThingsServer(custom, impl{}, app.WithRequestID("X-Request-ID"), app.WithErrorWriter(envelope)); err != nil {
		panic(err)
	}
	handler := app.ErrorHandler(custom, app.WithRequestID("X-Request-ID"), app.WithErrorWriter(envelope))

	status, body := do(handler, "POST", "/v1/things/save", "{not json", tenant)
	check("bad json status", status, 400)
	check("bad json code", errorOf(body)["code"], "invalid_request_body")
	if id, _ := errorOf(body)["request_id"].(string); id == "" {
		panic("the envelope must carry the request id")
	}

	status, body = do(handler, "GET", "/v1/things/abc", "", tenant)
	check("bad path status", status, 400)
	check("bad path code", errorOf(body)["code"], "invalid_path_parameter")
	check("bad path field", errorOf(body)["field"], "id")
	if msg, _ := errorOf(body)["message"].(string); strings.Contains(msg, "strconv") || strings.Contains(msg, "parsing") {
		panic("Go internals leaked: " + msg)
	}
	check("bad path message", errorOf(body)["message"], "invalid path parameter id: must be an integer")

	status, body = do(handler, "GET", "/v1/things/1?page=x", "", tenant)
	check("bad query code", errorOf(body)["code"], "invalid_query_parameter")

	status, body = do(handler, "GET", "/v1/things/1", "", nil)
	check("missing header code", errorOf(body)["code"], "missing_header")
	check("missing header field", errorOf(body)["field"], "X-Tenant")

	status, body = do(handler, "POST", "/v1/things/save", ` + "`" + `{"id":"1","name":"x"}` + "`" + `, tenant)
	check("validation status", status, 400)
	check("validation code", errorOf(body)["code"], "validation_failed")
	if violations, _ := errorOf(body)["violations"].([]any); len(violations) == 0 {
		panic("violations missing")
	}

	status, body = do(handler, "GET", "/v1/things/3", "", tenant)
	check("unknown error status", status, 500)
	check("unknown error code", errorOf(body)["code"], "internal")
	check("unknown error message", errorOf(body)["message"], "internal server error")
	if strings.Contains(fmt.Sprint(body), "secret") {
		panic("the cause must never be sent")
	}

	status, body = do(handler, "GET", "/v1/things/2", "", tenant)
	check("typed error status", status, 409)
	check("typed error body", errorOf(body)["request_id"], "typed")

	status, body = do(handler, "GET", "/nope", "", tenant)
	check("unmatched route", status, 404)
	check("unmatched code", errorOf(body)["code"], "not_found")
	status, body = do(handler, "DELETE", "/v1/things/save", "", tenant)
	check("wrong method", status, 405)
	check("wrong method code", errorOf(body)["code"], "method_not_allowed")

	plain := http.NewServeMux()
	if err := app.RegisterThingsServer(plain, impl{}); err != nil {
		panic(err)
	}
	status, body = do(plain, "POST", "/v1/things/save", "{not json", tenant)
	check("default status", status, 400)
	check("default body", fmt.Sprint(body), "map[message:invalid request body]")
	status, body = do(plain, "GET", "/v1/things/abc", "", tenant)
	check("default path body", body["message"], "invalid path parameter id: must be an integer")
	status, body = do(plain, "GET", "/v1/things/3", "", tenant)
	check("default internal body", fmt.Sprint(body), "map[message:internal server error]")
	fmt.Println("OK")
}
`

func TestGoServerErrorWriterCoversEveryErrorPath(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, errorWriterSchema)
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
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/errs\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "app", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "app", "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), errorWriterHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
