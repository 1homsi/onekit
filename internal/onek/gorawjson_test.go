package onek

import "testing"

// A raw route with request: "json" leaves the body to the handler, so the
// server must not validate the @body field it never read, and a handler that
// writes its own error body and status keeps them.
func TestGoRawJSONRouteHandlerOwnsTheBodyAndItsErrors(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Query {
  @minLength(3)
  sql: string;
}

model RunRequest {
  id: string;
  @body query: Query;
}

model RunResult {
  rows: int32;
}

@error
model RunFailed {
  @statusCode _: 422;
  error: string;
  logs: string[];
}

interface Queries {
  @post
  @route("/queries/{id}/run")
  @Onkt.http(#{ request: "json", response: "json" })
  run(...RunRequest): RunResult | RunFailed;
}
`, `package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type queries struct{}

func (queries) Run(w http.ResponseWriter, r *http.Request, req *RunRequest) error {
	var q Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	return json.NewEncoder(w).Encode(map[string]any{"error": "bad sql " + q.Sql, "logs": []string{"line 1"}, "result": nil})
}

func TestRawJSON(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterQueriesServer(mux, queries{}, WithErrorWriter(func(w http.ResponseWriter, r *http.Request, e *ServerError) {
		w.WriteHeader(599)
	})); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	resp, err := http.Post(server.URL+"/queries/7/run", "application/json", strings.NewReader(`+"`"+`{"sql":"x"}`+"`"+`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `+"`"+`"logs":["line 1"]`+"`"+`) {
		t.Fatalf("the handler's own error body must reach the client: %d %s", resp.StatusCode, body)
	}
}
`)
}
