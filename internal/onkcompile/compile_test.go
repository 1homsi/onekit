package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func parseOrFatal(t *testing.T, src string) *onklang.File {
	t.Helper()
	f, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return f
}

const modelsSrc = `
package examples.simpleapi.models

message Address {
  street: string
  city: string
}

message User {
  id: string
  name: string @len(2, 100)
  bio: string? @nullable
  tags: string[]
  labels: map[string, string]
  home_address: Address @flatten(prefix: "home_")
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message Event {
  id: string
  payload: oneof(discriminator: "type", flatten: true) {
    text: TextPayload @tag("text")
    image: ImagePayload @tag("image")
  }
}

message TextPayload {
  body: string
}

message ImagePayload {
  url: string
}

message NotFoundError @status(404) {
  resource_type: string
}

message ValidationError {
  field: string
}

message CreateUserRequest {
  name: string
}

message GetUserRequest {
  id: string @query("id")
}
`

const serviceSrc = `
package examples.simpleapi.services

import "./models.onk"

service UserService {
  base_path: "/api/v1"
  headers: {
    "X-API-Key": string @required @format("uuid") @auth("api_key")
  }

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User | NotFoundError | ValidationError @get("/users/{id}") {
    headers: {
      "X-Request-Id": string @required
    }
  }
}
`

func compileFixture(t *testing.T) *onkir.Package {
	t.Helper()
	sources := []Source{
		{Path: "models.onk", AST: parseOrFatal(t, modelsSrc)},
		{Path: "user_service.onk", AST: parseOrFatal(t, serviceSrc)},
	}
	pkg, err := Compile(sources)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	return pkg
}

func findMessage(f *onkir.File, name string) *onkir.Message {
	for _, m := range f.Messages {
		if m.Name == name {
			return m
		}
	}
	return nil
}

func TestCompileResolvesFieldTypes(t *testing.T) {
	pkg := compileFixture(t)
	models := pkg.Files[0]

	user := findMessage(models, "User")
	if user == nil {
		t.Fatalf("User message not found")
	}

	addrField := user.Fields[5]
	if addrField.Name != "home_address" || addrField.Type.Kind != onkir.KindMessage {
		t.Fatalf("unexpected home_address field: %+v", addrField)
	}
	if addrField.Type.Message.Name != "Address" {
		t.Fatalf("expected home_address to resolve to Address, got %s", addrField.Type.Message.Name)
	}

	labelsField := user.Fields[4]
	if labelsField.Type.Kind != onkir.KindMap || labelsField.Type.MapKey != onkir.ScalarString {
		t.Fatalf("unexpected labels field: %+v", labelsField.Type)
	}
	if labelsField.Type.MapValue.Kind != onkir.KindScalar || labelsField.Type.MapValue.Scalar != onkir.ScalarString {
		t.Fatalf("unexpected labels map value type: %+v", labelsField.Type.MapValue)
	}

	tagsField := user.Fields[3]
	if !tagsField.Repeated || tagsField.Type.Scalar != onkir.ScalarString {
		t.Fatalf("unexpected tags field: %+v", tagsField)
	}
}

func TestCompileResolvesOneofVariantTypes(t *testing.T) {
	pkg := compileFixture(t)
	models := pkg.Files[0]

	event := findMessage(models, "Event")
	if event == nil {
		t.Fatalf("Event message not found")
	}
	payload := event.Fields[1]
	if payload.Oneof == nil {
		t.Fatalf("expected payload to be a oneof")
	}
	if len(payload.Oneof.Variants) != 2 {
		t.Fatalf("expected 2 oneof variants, got %d", len(payload.Oneof.Variants))
	}
	textVariant := payload.Oneof.Variants[0]
	if textVariant.Type.Kind != onkir.KindMessage || textVariant.Type.Message.Name != "TextPayload" {
		t.Fatalf("unexpected text variant type: %+v", textVariant.Type)
	}
	if textVariant.Tag() != "text" {
		t.Fatalf("unexpected variant tag: %q", textVariant.Tag())
	}
	disc, ok := payload.Oneof.Discriminator()
	if !ok || disc != "type" {
		t.Fatalf("unexpected discriminator: %q, ok=%v", disc, ok)
	}
	if !payload.Oneof.Flatten() {
		t.Fatalf("expected oneof to be flattened")
	}
}

func TestCompileResolvesEnumFields(t *testing.T) {
	pkg := compileFixture(t)
	models := pkg.Files[0]
	if len(models.Enums) != 1 || models.Enums[0].Name != "Status" {
		t.Fatalf("unexpected enums: %+v", models.Enums)
	}
	status := models.Enums[0]
	if len(status.Values) != 2 || status.Values[1].JSONName() != "active" {
		t.Fatalf("unexpected enum values: %+v", status.Values)
	}
}

func TestCompileMessageLevelDecorator(t *testing.T) {
	pkg := compileFixture(t)
	models := pkg.Files[0]
	notFound := findMessage(models, "NotFoundError")
	if notFound == nil {
		t.Fatalf("NotFoundError message not found")
	}
	if !notFound.IsError() {
		t.Fatalf("expected NotFoundError to be recognized as an error")
	}
	code, ok := notFound.StatusCode()
	if !ok || code != 404 {
		t.Fatalf("unexpected status code: %d, ok=%v", code, ok)
	}
}

func TestCompileServiceAndMethods(t *testing.T) {
	pkg := compileFixture(t)
	services := pkg.Files[1]
	if len(services.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services.Services))
	}
	svc := services.Services[0]
	if svc.BasePath != "/api/v1" {
		t.Fatalf("unexpected base path: %q", svc.BasePath)
	}
	if len(svc.Headers) != 1 || svc.Headers[0].Name != "X-API-Key" || !svc.Headers[0].Required() {
		t.Fatalf("unexpected service headers: %+v", svc.Headers)
	}
	if len(svc.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(svc.Methods))
	}

	create := svc.Methods[0]
	if create.Request.Name != "CreateUserRequest" || create.Response.Name != "User" {
		t.Fatalf("unexpected createUser method: %+v", create)
	}
	verb, ok := create.Verb()
	if !ok || verb != "post" {
		t.Fatalf("unexpected createUser verb: %q, ok=%v", verb, ok)
	}

	get := svc.Methods[1]
	if len(get.ErrorTypes) != 2 || get.ErrorTypes[0].Name != "NotFoundError" ||
		get.ErrorTypes[1].Name != "ValidationError" {
		t.Fatalf("unexpected getUser error types: %+v", get.ErrorTypes)
	}
	if len(get.Headers) != 1 || get.Headers[0].Name != "X-Request-Id" {
		t.Fatalf("unexpected getUser headers: %+v", get.Headers)
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		name    string
		sources []Source
		wantErr string
	}{
		{
			name: "duplicate message name",
			sources: []Source{
				{Path: "a.onk", AST: parseOrFatal(t, "message User {\n  id: string\n}\n")},
				{Path: "b.onk", AST: parseOrFatal(t, "message User {\n  id: string\n}\n")},
			},
			wantErr: "duplicate message name",
		},
		{
			name: "unresolved field type",
			sources: []Source{
				{Path: "a.onk", AST: parseOrFatal(t, "message User {\n  addr: Address\n}\n")},
			},
			wantErr: "unresolved type",
		},
		{
			name: "unresolved rpc request type",
			sources: []Source{
				{
					Path: "a.onk",
					AST: parseOrFatal(
						t,
						"message User {\n  id: string\n}\nservice S {\n  create(Missing) -> User @post(\"/x\")\n}\n",
					),
				},
			},
			wantErr: "unresolved request type",
		},
		{
			name: "unresolved rpc response type",
			sources: []Source{
				{
					Path: "a.onk",
					AST: parseOrFatal(
						t,
						"message User {\n  id: string\n}\nservice S {\n  create(User) -> Missing @post(\"/x\")\n}\n",
					),
				},
			},
			wantErr: "unresolved response type",
		},
		{
			name: "unresolved rpc error type",
			sources: []Source{
				{
					Path: "a.onk",
					AST: parseOrFatal(
						t,
						"message User {\n  id: string\n}\nservice S {\n  create(User) -> User | Missing @post(\"/x\")\n}\n",
					),
				},
			},
			wantErr: "unresolved error type",
		},
		{
			name: "invalid header type",
			sources: []Source{
				{
					Path: "a.onk",
					AST: parseOrFatal(
						t,
						"message User {\n  id: string\n}\nservice S {\n  headers: {\n    \"X-A\": weirdtype\n  }\n  create(User) -> User @post(\"/x\")\n}\n",
					),
				},
			},
			wantErr: "invalid header type",
		},
		{
			name: "message and enum name collision",
			sources: []Source{
				{Path: "a.onk", AST: parseOrFatal(t, "message User {\n  id: string\n}\nenum User {\n  A\n}\n")},
			},
			wantErr: "already used",
		},
		{
			name: "invalid map key type",
			sources: []Source{
				{Path: "a.onk", AST: parseOrFatal(t, "message User {\n  m: map[Weird, string]\n}\n")},
			},
			wantErr: "invalid map key type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.sources)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestCompileAllowsSameMessageNameInDifferentDirectories(t *testing.T) {
	sources := []Source{
		{
			Path: "booking/dashboard/v1/models.onk",
			AST:  parseOrFatal(t, "message GetDashboardRequest {\n  id: string\n}\n"),
		},
		{
			Path: "crm/dashboard/v1/models.onk",
			AST:  parseOrFatal(t, "message GetDashboardRequest {\n  name: string\n}\n"),
		},
	}
	pkg, err := Compile(sources)
	if err != nil {
		t.Fatalf("expected same-named messages in different directories to compile, got: %v", err)
	}
	booking := findMessage(pkg.Files[0], "GetDashboardRequest")
	crm := findMessage(pkg.Files[1], "GetDashboardRequest")
	if booking == nil || crm == nil {
		t.Fatalf("expected both GetDashboardRequest messages to be found")
	}
	if len(booking.Fields) != 1 || booking.Fields[0].Name != "id" {
		t.Fatalf("unexpected booking GetDashboardRequest fields: %+v", booking.Fields)
	}
	if len(crm.Fields) != 1 || crm.Fields[0].Name != "name" {
		t.Fatalf("unexpected crm GetDashboardRequest fields: %+v", crm.Fields)
	}
}

func TestCompileResolvesUniqueCrossDirectoryReferenceByName(t *testing.T) {
	sources := []Source{
		{Path: "common/pagination/v1/models.onk", AST: parseOrFatal(t, "message PageInfo {\n  next: string\n}\n")},
		{
			Path: "hub/business/v1/models.onk",
			AST:  parseOrFatal(t, "message ListBusinessesResponse {\n  page: PageInfo\n}\n"),
		},
	}
	pkg, err := Compile(sources)
	if err != nil {
		t.Fatalf("unexpected compile error resolving cross-directory reference: %v", err)
	}
	resp := findMessage(pkg.Files[1], "ListBusinessesResponse")
	if resp == nil {
		t.Fatalf("ListBusinessesResponse not found")
	}
	pageField := resp.Fields[0]
	if pageField.Type.Kind != onkir.KindMessage || pageField.Type.Message.Name != "PageInfo" {
		t.Fatalf(
			"expected page field to resolve to the PageInfo message from a different directory, got: %+v",
			pageField.Type,
		)
	}
}

func TestCompileErrorsOnAmbiguousCrossDirectoryReference(t *testing.T) {
	sources := []Source{
		{Path: "crm/settings/v1/models.onk", AST: parseOrFatal(t, "message Settings {\n  a: string\n}\n")},
		{Path: "support/settings/v1/models.onk", AST: parseOrFatal(t, "message Settings {\n  b: string\n}\n")},
		{Path: "erp/warehouse/v1/models.onk", AST: parseOrFatal(t, "message Warehouse {\n  settings: Settings\n}\n")},
	}
	_, err := Compile(sources)
	if err == nil {
		t.Fatalf("expected an ambiguous type error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous type") {
		t.Fatalf("expected error to mention ambiguity, got: %q", err.Error())
	}
}

func TestCompileNestedMessagesAndEnums(t *testing.T) {
	src := `
message Outer {
  id: string
  message Inner {
    value: string
  }
  enum Kind {
    A
    B
  }
  inner: Inner
  kind: Kind
}
`
	f := parseOrFatal(t, src)
	pkg, err := Compile([]Source{{Path: "a.onk", AST: f}})
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	outer := findMessage(pkg.Files[0], "Outer")
	if outer == nil {
		t.Fatalf("Outer message not found")
	}
	if len(outer.Nested) != 1 || outer.Nested[0].Name != "Inner" {
		t.Fatalf("unexpected nested messages: %+v", outer.Nested)
	}
	if len(outer.NestedEnums) != 1 || outer.NestedEnums[0].Name != "Kind" {
		t.Fatalf("unexpected nested enums: %+v", outer.NestedEnums)
	}
	if outer.Nested[0].Parent != outer {
		t.Fatalf("expected Inner's parent to be Outer")
	}

	innerField := outer.Fields[1]
	if innerField.Type.Kind != onkir.KindMessage || innerField.Type.Message != outer.Nested[0] {
		t.Fatalf("unexpected inner field type: %+v", innerField.Type)
	}
	kindField := outer.Fields[2]
	if kindField.Type.Kind != onkir.KindEnum || kindField.Type.Enum != outer.NestedEnums[0] {
		t.Fatalf("unexpected kind field type: %+v", kindField.Type)
	}

	fullName := outer.Nested[0].FullName()
	if fullName != "Outer.Inner" {
		t.Fatalf("unexpected full name: %q", fullName)
	}
}
