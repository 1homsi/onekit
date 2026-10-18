package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoServerBodyDecodeErrorNamesPathAndProblem(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, `package app

enum ResourceType {
  REST @json("rest")
  S3 @json("s3")
  SQL @json("sql")
}

message SqlOptions {
  host: string
  port: int32
}

message Options {
  sql: SqlOptions?
}

message CreateResource {
  name: string
  type: ResourceType
  options: Options?
  tags: string[]
  labels: map[string, SqlOptions]
  size: int64
  created: timestamp?
  ratio: float32
}

message Resource { name: string }

message Wrapped {
  id: string
  options: Options
}

message Counted {
  id: string
  count: int64[]
}

service Resources {
  create(CreateResource) -> Resource @post("/api/v1/resources")
  wrap(Wrapped) -> Resource @put("/api/v1/wrapped/{id}") @body("options")
  count(Counted) -> Resource @put("/api/v1/counted/{id}") @body("count")
}
`)
	types, err := GenerateTypes(file)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateServer(file)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/bodyerr\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.gen.go"), string(types))
	writeFile(t, filepath.Join(dir, "validate.gen.go"), string(validation))
	writeFile(t, filepath.Join(dir, "server.gen.go"), string(server))
	writeFile(t, filepath.Join(dir, "body_test.go"), `package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type impl struct{}

func (impl) Create(ctx context.Context, req *CreateResource) (*Resource, error) {
	return &Resource{Name: req.Name}, nil
}

func (impl) Wrap(ctx context.Context, req *Wrapped) (*Resource, error) {
	return &Resource{Name: req.Id}, nil
}

func (impl) Count(ctx context.Context, req *Counted) (*Resource, error) {
	return &Resource{Name: req.Id}, nil
}

func TestBodyErrors(t *testing.T) {
	mux := http.NewServeMux()
	writer := WithErrorWriter(func(w http.ResponseWriter, r *http.Request, e *ServerError) {
		w.WriteHeader(e.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": e.Code, "message": e.Message, "field": e.Field})
	})
	if err := RegisterResourcesServer(mux, impl{}, writer); err != nil {
		t.Fatal(err)
	}
	type bodyCase struct {
		path, body, message, field string
	}
	var cases []bodyCase
	for _, c := range []struct {
		body, message, field string
	}{
		{`+"`"+`{"name":"db","type":"mysql"}`+"`"+`, `+"`"+`type: "mysql" is not one of rest, s3, sql`+"`"+`, "type"},
		{`+"`"+`{"type":"sql","options":{"sql":{"host":"h","port":"5432"}}}`+"`"+`, "options.sql.port: expected integer, got string", "options.sql.port"},
		{`+"`"+`{"options":{"sql":{"port":3000000000}}}`+"`"+`, "options.sql.port: 3000000000 is out of range for int32", "options.sql.port"},
		{`+"`"+`{"options":{"sql":{"port":1.5}}}`+"`"+`, "options.sql.port: 1.5 is not an integer", "options.sql.port"},
		{`+"`"+`{"options":{"sql":[]}}`+"`"+`, "options.sql: expected object, got array", "options.sql"},
		{`+"`"+`{"tags":["a", 3]}`+"`"+`, "tags[1]: expected string, got number", "tags[1]"},
		{`+"`"+`{"labels":{"a.b":{"port":true}}}`+"`"+`, `+"`"+`labels["a.b"].port: expected integer, got boolean`+"`"+`, `+"`"+`labels["a.b"].port`+"`"+`},
		{`+"`"+`{"labels":{"primary":{"host":7}}}`+"`"+`, "labels.primary.host: expected string, got number", "labels.primary.host"},
		{`+"`"+`{"size":12}`+"`"+`, "size: expected integer string, got number", "size"},
		{`+"`"+`{"size":"12x"}`+"`"+`, `+"`"+`size: "12x" is not an integer`+"`"+`, "size"},
		{`+"`"+`{"created":"yesterday"}`+"`"+`, `+"`"+`created: "yesterday" is not an RFC 3339 timestamp`+"`"+`, "created"},
		{`+"`"+`{"ratio":"high"}`+"`"+`, "ratio: expected number, got string", "ratio"},
		{`+"`"+`[1]`+"`"+`, "expected object, got array", ""},
		{`+"`"+`{"type":`+"`"+`, "invalid JSON: unexpected end of JSON input", ""},
		{`+"`"+`{"type":"rest" "x"}`+"`"+`, "invalid JSON: invalid character '\"' after object key:value pair", ""},
	} {
		cases = append(cases, bodyCase{"/api/v1/resources", c.body, c.message, c.field})
	}
	cases = append(cases,
		bodyCase{"/api/v1/wrapped/a", `+"`"+`{"sql":{"port":"x"}}`+"`"+`, "sql.port: expected integer, got string", "sql.port"},
		bodyCase{"/api/v1/counted/a", `+"`"+`["1", 2]`+"`"+`, "[1]: expected integer string, got number", "[1]"},
		bodyCase{"/api/v1/counted/a", `+"`"+`{"n":1}`+"`"+`, "expected array, got object", ""},
	)
	for _, c := range cases {
		method := http.MethodPut
		if c.path == "/api/v1/resources" {
			method = http.MethodPost
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, c.path, strings.NewReader(c.body)))
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v %s", c.body, err, rec.Body.String())
		}
		if rec.Code != http.StatusBadRequest || got["code"] != "invalid_request_body" || got["message"] != c.message || got["field"] != c.field {
			t.Errorf("%s:\n got %d %v\nwant message %q field %q", c.body, rec.Code, got, c.message, c.field)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/resources", strings.NewReader(`+"`"+`{"name":"db","type":"sql","options":{"sql":{"port":5432}},"size":"12","created":"2026-01-02T03:04:05Z"}`+"`"+`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `+"`"+`"db"`+"`"+`) {
		t.Fatalf("valid body: %d %s", rec.Code, rec.Body.String())
	}
	var req CreateResource
	err := json.Unmarshal([]byte(`+"`"+`{"type":"ftp"}`+"`"+`), &req)
	if err == nil || err.Error() != `+"`"+`type: "ftp" is not one of rest, s3, sql`+"`"+` {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p, ok := err.(interface{ JSONPath() string }); !ok || p.JSONPath() != "type" {
		t.Fatalf("JSONPath: %#v", err)
	}
	var kind ResourceType
	if err := json.Unmarshal([]byte(`+"`"+`"ftp"`+"`"+`), &kind); err == nil || err.Error() != `+"`"+`"ftp" is not one of rest, s3, sql`+"`"+` {
		t.Fatalf("enum: %v", err)
	}
}
`)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
