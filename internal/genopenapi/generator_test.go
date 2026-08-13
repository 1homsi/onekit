package genopenapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const fixtureSrc = `
package app

message Address {
  street: string
  city: string
}

message User {
  id: string
  name: string @len(2, 100)
  email: string @email
  bio: string?
  tags: string[]
  labels: map[string, string]
  home_address: Address
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message CreateUserRequest {
  name: string @len(2, 100)
  email: string @email
}

message GetUserRequest {
  id: string
}

message NotFoundError @status(404) {
  resource_type: string
  resource_id: string
}

service UserService {
  base_path: "/api/v1"
  headers: {
    "X-API-Key": string @required @format("uuid")
  }

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User | NotFoundError @get("/users/{id}")
}
`

func compileFixture(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(fixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return pkg.Files[0]
}

func TestGenerateProducesValidOpenAPIDocument(t *testing.T) {
	file := compileFixture(t)
	out, err := Generate(file, Options{Title: "Test API", Version: "1.2.3"})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty output")
	}

	doc, err := libopenapi.NewDocument(out)
	if err != nil {
		t.Fatalf("libopenapi.NewDocument failed to parse generated spec: %v\n%s", err, out)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("BuildV3Model error: %v\n%s", err, out)
	}

	if model.Model.Info.Title != "Test API" {
		t.Fatalf("unexpected title: %q", model.Model.Info.Title)
	}
	if model.Model.Info.Version != "1.2.3" {
		t.Fatalf("unexpected version: %q", model.Model.Info.Version)
	}

	createPath, ok := model.Model.Paths.PathItems.Get("/api/v1/users")
	if !ok {
		t.Fatalf("expected /api/v1/users path item")
	}
	if createPath.Post == nil {
		t.Fatalf("expected POST operation on /api/v1/users")
	}
	if createPath.Post.RequestBody == nil {
		t.Fatalf("expected request body on createUser operation")
	}

	getPath, ok := model.Model.Paths.PathItems.Get("/api/v1/users/{id}")
	if !ok {
		t.Fatalf("expected /api/v1/users/{id} path item")
	}
	if getPath.Get == nil {
		t.Fatalf("expected GET operation on /api/v1/users/{id}")
	}
	if _, has404 := getPath.Get.Responses.Codes.Get("404"); !has404 {
		t.Fatalf("expected 404 response on getUser operation")
	}
	foundPathParam := false
	foundHeaderParam := false
	for _, p := range getPath.Get.Parameters {
		if p.Name == "id" && p.In == "path" {
			foundPathParam = true
		}
		if p.Name == "X-API-Key" && p.In == "header" {
			foundHeaderParam = true
		}
	}
	if !foundPathParam {
		t.Fatalf("expected path parameter %q on getUser, got %+v", "id", getPath.Get.Parameters)
	}
	if !foundHeaderParam {
		t.Fatalf("expected header parameter X-API-Key on getUser, got %+v", getPath.Get.Parameters)
	}

	if _, hasUser := model.Model.Components.Schemas.Get("app.User"); !hasUser {
		t.Fatalf("expected User schema in components")
	}
	if _, hasNotFound := model.Model.Components.Schemas.Get("app.NotFoundError"); !hasNotFound {
		t.Fatalf("expected NotFoundError schema in components")
	}
	if _, hasStatus := model.Model.Components.Schemas.Get("app.Status"); !hasStatus {
		t.Fatalf("expected Status schema in components")
	}
}

func TestGenerateRepeatedQueryParametersAsFormArrays(t *testing.T) {
	ast, err := onklang.Parse(`
package queryfixture

message Request {
  status: int32 @query
  tags: string[] @query("tag")
}

message Response {}

service QueryService {
  list(Request) -> Response @get("/items")
}
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "query.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	out, err := Generate(pkg.Files[0], Options{})
	if err != nil {
		t.Fatalf("generate fixture: %v", err)
	}
	doc, err := libopenapi.NewDocument(out)
	if err != nil {
		t.Fatalf("parse generated OpenAPI: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("build OpenAPI model: %v", err)
	}
	path, ok := model.Model.Paths.PathItems.Get("/items")
	if !ok || path.Get == nil || len(path.Get.Parameters) != 2 {
		t.Fatalf("unexpected query operation: %+v", path)
	}
	for _, parameter := range path.Get.Parameters {
		if parameter.Name == "tag" {
			if parameter.Style != "form" || parameter.Explode == nil || !*parameter.Explode {
				t.Fatalf("repeated query parameter lacks form/explode contract: %+v", parameter)
			}
			if parameter.Schema == nil || parameter.Schema.Schema() == nil || len(parameter.Schema.Schema().Type) != 1 || parameter.Schema.Schema().Type[0] != "array" {
				t.Fatalf("repeated query parameter is not an array schema: %+v", parameter.Schema)
			}
		}
	}
}

func TestGenerateJSONProducesValidJSON(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateJSON(file, Options{})
	if err != nil {
		t.Fatalf("GenerateJSON error: %v", err)
	}
	var parsed map[string]any
	if jsonErr := json.Unmarshal(out, &parsed); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, out)
	}
	if parsed["openapi"] != "3.1.0" {
		t.Fatalf("unexpected openapi version field: %v", parsed["openapi"])
	}
}

func TestGenerateCarriesContractSemantics(t *testing.T) {
	src := `
package app
message Payload { name: string @required @len(2, 20) @pattern("^[a-z]+$") }
message Request { payload: Payload }
message Response { ok: bool }
service API {
  headers: { "Authorization": string @required @auth("bearer") @auth_scheme_name("UserAuth") }
  create(Request) -> Response @post("/items") @body("payload")
}
`
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := Generate(pkg.Files[0], Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	spec := string(out)
	for _, want := range []string{
		"UserAuth:", "scheme: bearer", "security:", "- UserAuth: []", "required:", "- name",
		"minLength: 2", "maxLength: 20", "pattern: ^[a-z]+$", "$ref: '#/components/schemas/app.Payload'",
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("generated OpenAPI missing %q:\n%s", want, spec)
		}
	}
	if strings.Contains(spec, "name: Authorization\n") {
		t.Fatalf("auth header should be represented by a security scheme, not a duplicate parameter:\n%s", spec)
	}
}
