package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// compileSources compiles TypeSpec files together and returns them as
// compiler sources, in path order.
func compileSources(t *testing.T, files map[string]string) []Source {
	t.Helper()
	var sources []Source
	for _, file := range onktest.Files(t, files) {
		sources = append(sources, Source{Path: file.Path, AST: &file.File})
	}
	return sources
}

func parseOrFatal(t *testing.T, src string) *onkschema.File {
	t.Helper()
	f, err := onktest.Parse(src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return f
}

func TestCompileAllowsLegacyContractFieldsWhenEnabled(t *testing.T) {
	source := Source{Path: "api.tsp", AST: parseOrFatal(t, `
model LegacyRequest {
  @Onkt.required
  @encode(string)
  id: int64;
  from: string;
  nickname?: string | null;
}
`)}

	if _, err := Compile([]Source{source}); err == nil || !strings.Contains(err.Error(), "@required on non-string scalars") {
		t.Fatalf("expected strict compile to reject legacy scalar presence, got %v", err)
	}
	if _, err := CompileWithOptions([]Source{source}, CompileOptions{AllowLegacyContracts: true}); err != nil {
		t.Fatalf("legacy compile rejected existing contract: %v", err)
	}
}

func TestCompileAllowsEmptyStringInSet(t *testing.T) {
	source := Source{Path: "api.tsp", AST: parseOrFatal(t, `
model Request {
  provider: "" | "google";
}
`)}

	if _, err := Compile([]Source{source}); err != nil {
		t.Fatalf("compile rejected an empty string @in value: %v", err)
	}
}

func TestCompileScopesDefaultRoutesByPackage(t *testing.T) {
	sources := []Source{
		{Path: "booking/dashboard/service.tsp", AST: parseOrFatal(t, `namespace booking.dashboard.v1;

model Request {}

model Response {}

interface Dashboard {
  @get
  @route("/dashboard")
  get(...Request): booking.dashboard.v1.Response;
}
`)},
		{Path: "crm/dashboard/service.tsp", AST: parseOrFatal(t, `namespace crm.dashboard.v1;

model Request {}

model Response {}

interface Dashboard {
  @get
  @route("/dashboard")
  get(...Request): crm.dashboard.v1.Response;
}
`)},
	}

	if _, err := Compile(sources); err != nil {
		t.Fatalf("compile rejected package-scoped default routes: %v", err)
	}
}

const modelsSrc = `namespace examples.simpleapi.models;

model Address {
  street: string;
  city: string;
}

model User {
  id: string;
  @minLength(2)
  @maxLength(100)
  name: string;
  bio?: string;
  tags: string[];
  labels: Record<string>;
  @Onkt.flatten("home_")
  home_address: Address;
}

enum Status {
  UNSPECIFIED,
  ACTIVE: "active",
}

@discriminated(#{ discriminatorPropertyName: "type", envelope: "none" })
union EventPayload {
  @Onkt.tag("text") text: TextPayload,
  @Onkt.tag("image") image: ImagePayload,
}

model Event {
  id: string;
  payload: EventPayload;
}

model TextPayload {
  body: string;
}

model ImagePayload {
  url: string;
}

@error
model NotFoundError {
  @statusCode _: 404;
  resource_type: string;
}

@error
model ValidationError {
  field: string;
}

model CreateUserRequest {
  name: string;
}

model GetUserRequest {
  id: string;
}
`

const serviceSrc = `using examples.simpleapi.models;

namespace examples.simpleapi.services;

/** Headers every UserService operation takes. */
alias UserServiceHeaders = {
  @header("X-API-Key") @format("uuid") @Onkt.auth("api_key") header_x_api_key: string;
};

@route("/api/v1")
interface UserService {
  @post
  @route("/users")
  createUser(...UserServiceHeaders, ...CreateUserRequest): User;

  @get
  @route("/users/{id}")
  getUser(
    ...UserServiceHeaders,
    @header("X-Request-Id") header_x_request_id: string,
    ...GetUserRequest,
  ): User | NotFoundError | ValidationError;
}
`

func compileFixture(t *testing.T) *onkir.Package {
	t.Helper()
	sources := compileSources(t, map[string]string{"models.tsp": modelsSrc, "user_service.tsp": serviceSrc})
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

func TestCompileSupportsJSONScalar(t *testing.T) {
	ast := parseOrFatal(t, `namespace examples.json;

model Envelope {
  payload: unknown;
  attributes: Record<unknown>;
  items: unknown[];
}
`)
	pkg, err := Compile([]Source{{Path: "json.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	message := pkg.Files[0].Messages[0]
	if message.Fields[0].Type.Scalar != onkir.ScalarJSON {
		t.Fatalf("expected json scalar, got %+v", message.Fields[0].Type)
	}
	if message.Fields[1].Type.MapValue.Scalar != onkir.ScalarJSON {
		t.Fatalf("expected json map value, got %+v", message.Fields[1].Type.MapValue)
	}
	if message.Fields[2].Type.Scalar != onkir.ScalarJSON || !message.Fields[2].Repeated {
		t.Fatalf("expected repeated json scalar, got %+v", message.Fields[2])
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

// Documents the compiler must reject whatever frontend wrote them; TypeSpec
// stops most of these itself, so they are built directly.
func TestCompileErrors(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	field, message, file := onktest.Field, onktest.Message, onktest.File
	post := dec("post", q("/x"))
	user := message("User", field("id", "string"))
	cases := []struct {
		name    string
		sources []Source
		wantErr string
	}{
		{
			name: "duplicate message name",
			sources: []Source{
				{Path: "a.tsp", AST: file("", message("User", field("id", "string")))},
				{Path: "b.tsp", AST: file("", message("User", field("id", "string")))},
			},
			wantErr: "duplicate message name",
		},
		{
			name:    "unresolved field type",
			sources: []Source{{Path: "a.tsp", AST: file("", message("User", field("addr", "Address")))}},
			wantErr: "unresolved type",
		},
		{
			name:    "unresolved rpc request type",
			sources: []Source{{Path: "a.tsp", AST: file("", user, onktest.Service("S", "", onktest.RPC("create", "Missing", "User", post)))}},
			wantErr: "unresolved request type",
		},
		{
			name:    "unresolved rpc response type",
			sources: []Source{{Path: "a.tsp", AST: file("", user, onktest.Service("S", "", onktest.RPC("create", "User", "Missing", post)))}},
			wantErr: "unresolved response type",
		},
		{
			name:    "unresolved rpc error type",
			sources: []Source{{Path: "a.tsp", AST: file("", user, onktest.Service("S", "", onktest.RPC("create", "User", "User|Missing", post)))}},
			wantErr: "unresolved error type",
		},
		{
			name: "invalid header type",
			sources: []Source{{Path: "a.tsp", AST: func() *onkschema.File {
				service := onktest.Service("S", "", onktest.RPC("create", "User", "User", post))
				service.Headers = []onkschema.HeaderDecl{{Name: "X-A", Type: "weirdtype"}}
				return file("", user, service)
			}()}},
			wantErr: "invalid header type",
		},
		{
			name:    "message and enum name collision",
			sources: []Source{{Path: "a.tsp", AST: file("", message("User", field("id", "string")), onktest.Enum("User", "A"))}},
			wantErr: "already used",
		},
		{
			name:    "invalid map key type",
			sources: []Source{{Path: "a.tsp", AST: file("", message("User", field("m", "map<Weird, string>")))}},
			wantErr: "invalid map key type",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.sources)
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestCompileAllowsSameMessageNameInDifferentDirectories(t *testing.T) {
	pkg, err := Compile(compileSources(t, map[string]string{
		"booking/dashboard/v1/models.tsp": "namespace booking.dashboard.v1;\nmodel GetDashboardRequest { id: string; }\n",
		"crm/dashboard/v1/models.tsp":     "namespace crm.dashboard.v1;\nmodel GetDashboardRequest { name: string; }\n",
	}))
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

// A document may name a type declared in another directory without its
// package (TypeSpec always qualifies); it resolves when the name is unique.
func TestCompileResolvesUniqueCrossDirectoryReferenceByName(t *testing.T) {
	field, message, file := onktest.Field, onktest.Message, onktest.File
	pkg, err := Compile([]Source{
		{Path: "common/pagination/v1/models.tsp", AST: file("", message("PageInfo", field("next", "string")))},
		{Path: "hub/business/v1/models.tsp", AST: file("", message("ListBusinessesResponse", field("page", "PageInfo")))},
	})
	if err != nil {
		t.Fatalf("unexpected compile error resolving cross-directory reference: %v", err)
	}
	resp := findMessage(pkg.Files[1], "ListBusinessesResponse")
	if resp == nil {
		t.Fatalf("ListBusinessesResponse not found")
	}
	pageField := resp.Fields[0]
	if pageField.Type.Kind != onkir.KindMessage || pageField.Type.Message.Name != "PageInfo" {
		t.Fatalf("expected page field to resolve to the PageInfo message from a different directory, got: %+v", pageField.Type)
	}
}

func TestCompileErrorsOnAmbiguousCrossDirectoryReference(t *testing.T) {
	field, message, file := onktest.Field, onktest.Message, onktest.File
	_, err := Compile([]Source{
		{Path: "crm/settings/v1/models.tsp", AST: file("", message("Settings", field("a", "string")))},
		{Path: "support/settings/v1/models.tsp", AST: file("", message("Settings", field("b", "string")))},
		{Path: "erp/warehouse/v1/models.tsp", AST: file("", message("Warehouse", field("settings", "Settings")))},
	})
	if err == nil {
		t.Fatalf("expected an ambiguous type error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous type") {
		t.Fatalf("expected error to mention ambiguity, got: %q", err.Error())
	}
}

func TestCompileResolvesPackageQualifiedReferenceDespiteAmbiguity(t *testing.T) {
	pkg, err := Compile(compileSources(t, map[string]string{
		"crm/settings/v1/models.tsp":     "namespace crm.settings.v1;\nmodel Settings { a: string; }\n",
		"support/settings/v1/models.tsp": "namespace support.settings.v1;\nmodel Settings { b: string; }\n",
		"erp/warehouse/v1/models.tsp":    "namespace erp.warehouse.v1;\nmodel Warehouse { settings: support.settings.v1.Settings; }\n",
	}))
	if err != nil {
		t.Fatalf("expected a package-qualified reference to resolve despite the ambiguous plain name, got: %v", err)
	}
	warehouse := findMessage(pkg.Files[1], "Warehouse")
	if warehouse == nil {
		t.Fatalf("Warehouse message not found")
	}
	settingsField := warehouse.Fields[0]
	if settingsField.Type.Kind != onkir.KindMessage || settingsField.Type.Message.Name != "Settings" {
		t.Fatalf("unexpected settings field type: %+v", settingsField.Type)
	}
	if len(settingsField.Type.Message.Fields) != 1 || settingsField.Type.Message.Fields[0].Name != "b" {
		t.Fatalf("expected the qualified reference to resolve to support.settings.v1's Settings (field %q), got: %+v", "b", settingsField.Type.Message.Fields)
	}
}

// Import scopes come from documents that list their imports (TypeSpec
// resolves every reference itself and lists none).
func TestCompileValidatesLocalImports(t *testing.T) {
	api := onktest.File("", onktest.Service("API", ""))
	api.Imports = []string{"../missing.tsp"}
	_, err := Compile([]Source{{Path: "api/service.tsp", AST: api}})
	if err == nil || !strings.Contains(err.Error(), "does not match a discovered schema file") {
		t.Fatalf("expected missing import error, got %v", err)
	}
}

func TestCompileResolvesOnlyImportedDirectoriesWhenImportsAreDeclared(t *testing.T) {
	q, dec, field, message, file := onktest.Q, onktest.Dec, onktest.Field, onktest.Message, onktest.File
	api := file("api",
		message("Request", field("value", "string", dec("query"))),
		onktest.Service("API", "", onktest.RPC("get", "Request", "Shared", dec("get", q("/shared")))),
	)
	api.Imports = []string{"../common/models.tsp"}
	pkg, err := Compile([]Source{
		{Path: "common/models.tsp", AST: file("common", message("Shared", field("value", "string")))},
		{Path: "other/models.tsp", AST: file("other", message("Shared", field("other", "string")))},
		{Path: "api/service.tsp", AST: api},
	})
	if err != nil {
		t.Fatalf("expected imported type to resolve: %v", err)
	}
	method := pkg.Files[2].Services[0].Methods[0]
	if method.Response == nil || method.Response.File == nil || method.Response.File.Package != "common" {
		t.Fatalf("expected response from imported common package, got %+v", method.Response)
	}
}

func TestCompileRejectsImportCycles(t *testing.T) {
	a := onktest.File("", onktest.Message("A"))
	a.Imports = []string{"b.tsp"}
	b := onktest.File("", onktest.Message("B"))
	b.Imports = []string{"a.tsp"}
	_, err := Compile([]Source{{Path: "a.tsp", AST: a}, {Path: "b.tsp", AST: b}})
	if err == nil || !strings.Contains(err.Error(), "cyclic schema import") {
		t.Fatalf("expected import cycle error, got %v", err)
	}
}

func TestCompileRejectsInvalidSchemaSemantics(t *testing.T) {
	q, n, dec, field, message, file := onktest.Q, onktest.N, onktest.Dec, onktest.Field, onktest.Message, onktest.File
	api := func(rpcs ...*onkschema.RPCDecl) *onkschema.File {
		return file("", message("Request"), message("Response"), onktest.Service("API", "", rpcs...))
	}
	tests := []struct {
		name    string
		file    *onkschema.File
		wantErr string
	}{
		{"unknown field decorator", file("", message("Request", field("id", "string", dec("requred")))), "unknown decorator @requred"},
		{"invalid decorator argument count", file("", message("Request", field("name", "string", dec("len", n("1"))))), "@len expects 2 argument(s)"},
		{"missing HTTP verb", api(onktest.RPC("get", "Request", "Response")), "RPC must declare exactly one HTTP verb"},
		{
			"duplicate HTTP route",
			api(onktest.RPC("first", "Request", "Response", dec("get", q("/items"))), onktest.RPC("second", "Request", "Response", dec("get", q("/items")))),
			"duplicate HTTP route GET /items",
		},
		{"multiple HTTP verbs", api(onktest.RPC("get", "Request", "Response", dec("get", q("/items")), dec("post", q("/items")))), "RPC must declare exactly one HTTP verb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.tsp", AST: tt.file}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCompileRejectsFlattenCombinedWithEmpty(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model Meta { street: string; }
model Request { @Onkt.flatten("m_") @Onkt.empty("null") meta: Meta; }
model Result { ok: boolean; }
interface API { @post @route("/x") create(...Request): Result; }
`)}})
	if err == nil || !strings.Contains(err.Error(), "@flatten cannot be combined with @empty") {
		t.Fatalf("expected the combination to be rejected, got %v", err)
	}
}

func TestCompileRejectsMapValueUnwrap(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model Entry {
  @Onkt.unwrap
  value: string;
}

model Catalog {
  entries: Record<Entry>;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "@unwrap is not supported on map value message") {
		t.Fatalf("expected map-value unwrap error, got %v", err)
	}
}

func TestCompileNestedMessagesAndEnums(t *testing.T) {
	src := `
model Outer {
  id: string;
  inner: Inner;
  kind: Kind;
}

@Onkt.parent(Outer)
model Inner {
  value: string;
}

@Onkt.parent(Outer)
enum Kind {
  A,
  B,
}
`
	f := parseOrFatal(t, src)
	pkg, err := Compile([]Source{{Path: "a.tsp", AST: f}})
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

func TestCompileResolvesPackageQualifiedRPCReferencesAndMarksErrors(t *testing.T) {
	pkg, err := Compile(compileSources(t, map[string]string{
		"models.tsp": `namespace app.models;
model Request { id: string; }
model Result { id: string; }
@error model Failure { @statusCode _: 422; message: string; }
`,
		"service.tsp": `using app.models;

namespace app.service;
interface API {
  @get @route("/items/{id}") fetch(...app.models.Request): app.models.Result | app.models.Failure;
}
`,
	}))
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	method := pkg.Files[1].Services[0].Methods[0]
	if got := method.Request.FullName(); got != "app.models.Request" {
		t.Fatalf("request full name = %q", got)
	}
	if got := method.Response.FullName(); got != "app.models.Result" {
		t.Fatalf("response full name = %q", got)
	}
	if len(method.ErrorTypes) != 1 || method.ErrorTypes[0].FullName() != "app.models.Failure" || !method.ErrorTypes[0].IsError() {
		t.Fatalf("qualified error was not resolved and marked: %+v", method.ErrorTypes)
	}
}

func TestCompileRejectsCrossTargetContractHazards(t *testing.T) {
	tests := []struct {
		name, schema, want string
	}{
		{"non-string map key", `model M { @Onkt.mapKey("int64") values: Record<string>; }`, ""},
		{"duplicate error status", `
model Req {}
model Resp {}
@error model A { @statusCode _: 409; }
@error model B { @statusCode _: 409; }
interface API { @get @route("/items") get(...Req): Resp | A | B; }
`, "use the same HTTP status 409"},
		{"backtracking pattern", `model M { @pattern("^(a+)+$") value: string; }`, "nested repetition that is unsafe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, tt.schema)}})
			if tt.want == "" {
				if err != nil {
					t.Fatalf("integer map keys must compile: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

// Hazards TypeSpec cannot write: a float map key, @body on a GET (TypeSpec
// reads a lone @body as the request), an unknown @auth kind, and a route
// parameter no request field fills (TypeSpec's HTTP library rejects it).
func TestCompileRejectsCrossTargetContractHazardsInDocuments(t *testing.T) {
	q, dec, field, message, file := onktest.Q, onktest.Dec, onktest.Field, onktest.Message, onktest.File
	authService := onktest.Service("API", "", onktest.RPC("get", "Req", "Resp", dec("get", q("/items"))))
	authService.Headers = []onkschema.HeaderDecl{onktest.Header("Authorization", dec("required"), dec("auth", q("oauth")))}
	tests := []struct {
		name string
		file *onkschema.File
		want string
	}{
		{"non-string map key", file("", message("M", field("values", "map<float64, string>"))), "map keys must be string, int32"},
		{
			"body on get",
			file("", message("Req", field("value", "string")), message("Resp"),
				onktest.Service("API", "", onktest.RPC("get", "Req", "Resp", dec("get", q("/items")), dec("body", q("value"))))),
			"@body requires a body-bearing HTTP verb",
		},
		{"invalid auth kind", file("", message("Req"), message("Resp"), authService), "@auth must be api_key, bearer, or basic"},
		{
			"missing path field",
			file("", message("Req"), message("Resp"), onktest.Service("API", "", onktest.RPC("get", "Req", "Resp", dec("get", q("/items/{id}"))))),
			`path parameter "id" on RPC get requires one non-repeated scalar request field`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.tsp", AST: tt.file}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

// A schema with no imports of its own is deliberately absent from the published
// import scopes, so it must not double as the recursion memo: sharing the two
// handed the second and later importers a nil scope.
func TestCompileResolvesSharedImportForEveryImporter(t *testing.T) {
	q, dec, field, message, file := onktest.Q, onktest.Dec, onktest.Field, onktest.Message, onktest.File
	a := file("a", message("AReq", field("id", "string")), message("Invoice", field("total", "Money")),
		onktest.Service("AService", "", onktest.RPC("get", "AReq", "Invoice", dec("post", q("/get")))))
	a.Imports = []string{"../common/money.tsp"}
	b := file("b", message("BReq", field("id", "string")), message("Receipt", field("paid", "Money")),
		onktest.Service("BService", "", onktest.RPC("get", "BReq", "Receipt", dec("post", q("/get")))))
	b.Imports = []string{"../common/money.tsp"}
	pkg, err := Compile([]Source{
		{Path: "common/money.tsp", AST: file("common", message("Money", field("amount", "int64")))},
		{Path: "a/service.tsp", AST: a},
		{Path: "b/service.tsp", AST: b},
	})
	if err != nil {
		t.Fatalf("every importer of an import-free schema must resolve it: %v", err)
	}

	for _, want := range []struct{ pkgName, message, field string }{
		{"a", "Invoice", "total"},
		{"b", "Receipt", "paid"},
	} {
		var field *onkir.Field
		for _, file := range pkg.Files {
			if file.Package != want.pkgName {
				continue
			}
			for _, message := range file.Messages {
				if message.Name != want.message {
					continue
				}
				for _, candidate := range message.Fields {
					if candidate.Name == want.field {
						field = candidate
					}
				}
			}
		}
		if field == nil {
			t.Fatalf("%s.%s.%s not found", want.pkgName, want.message, want.field)
		}
		if field.Type == nil || field.Type.Message == nil || field.Type.Message.Name != "Money" {
			t.Fatalf("%s.%s.%s should resolve to the shared Money message, got %+v", want.pkgName, want.message, want.field, field.Type)
		}
	}
}
