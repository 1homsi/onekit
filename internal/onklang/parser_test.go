package onklang

import "testing"

const sampleModels = `
package examples.simpleapi.models

message Address {
  street: string
  city: string
}

message User {
  id: string
  name: string @len(2, 100)
  email: string @email
  bio: string? @nullable
  tags: string[]
  labels: map[string, string]
  home_address: Address @flatten(prefix: "home_")
  balance_cents: int64 @encode(number)
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
  INACTIVE @json("inactive")
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

message NotFoundError {
  resource_type: string
  resource_id: string
}

enum Timeframe {
  TIMEFRAME_UNSPECIFIED
  TIMEFRAME_1D
  TIMEFRAME_1W
}

message GetPortfolioRequest {
  timeframe: Timeframe @query("timeframe") @required
  account_id: string? @query("account_id")
}
`

const sampleService = `
package examples.simpleapi.services

import "./models.onk"

service UserService {
  base_path: "/api/v1"

  headers: {
    "X-API-Key": string @required @format("uuid") @auth("api_key")
  }

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User @get("/users/{id}") {
    headers: {
      "X-Request-Id": string @required @format("uuid")
    }
  }

  updateProfile(UpdateProfileRequest) -> Profile @put("/users/{user_id}/profile") @body("profile")

  streamEvents(StreamEventsRequest) -> Event @get("/events") @stream

  searchUsers(SearchUsersRequest) -> SearchUsersResponse @query("/users/search")
}
`

func TestParseModels(t *testing.T) {
	f, err := Parse(sampleModels)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if f.Package != "examples.simpleapi.models" {
		t.Fatalf("package = %q", f.Package)
	}
	if len(f.Messages) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(f.Messages))
	}

	user := f.Messages[1]
	if user.Name != "User" {
		t.Fatalf("expected User message, got %s", user.Name)
	}
	if len(user.Fields) != 8 {
		t.Fatalf("expected 8 fields on User, got %d", len(user.Fields))
	}

	nameField := user.Fields[1]
	if nameField.Type.Name != "string" {
		t.Fatalf("name field type = %+v", nameField.Type)
	}
	if len(nameField.Decorators) != 1 || nameField.Decorators[0].Name != "len" {
		t.Fatalf("unexpected decorators on name field: %+v", nameField.Decorators)
	}
	if len(nameField.Decorators[0].Args) != 2 || nameField.Decorators[0].Args[0].Value != "2" || nameField.Decorators[0].Args[1].Value != "100" {
		t.Fatalf("unexpected len() args: %+v", nameField.Decorators[0].Args)
	}

	emailField := user.Fields[2]
	if len(emailField.Decorators) != 1 || emailField.Decorators[0].Name != "email" || emailField.Decorators[0].Args != nil {
		t.Fatalf("unexpected email field: %+v", emailField)
	}

	bioField := user.Fields[3]
	if !bioField.Optional {
		t.Fatalf("expected bio field to be optional")
	}
	if len(bioField.Decorators) != 1 || bioField.Decorators[0].Name != "nullable" {
		t.Fatalf("unexpected bio decorators: %+v", bioField.Decorators)
	}

	tagsField := user.Fields[4]
	if !tagsField.Repeated || tagsField.Type.Name != "string" {
		t.Fatalf("unexpected tags field: %+v", tagsField)
	}

	labelsField := user.Fields[5]
	if !labelsField.Type.IsMap || labelsField.Type.MapKey != "string" || labelsField.Type.MapVal.Name != "string" {
		t.Fatalf("unexpected map type: %+v", labelsField.Type)
	}

	addrField := user.Fields[6]
	if len(addrField.Decorators) != 1 || addrField.Decorators[0].Name != "flatten" {
		t.Fatalf("unexpected flatten decorator: %+v", addrField.Decorators)
	}
	if len(addrField.Decorators[0].Args) != 1 || addrField.Decorators[0].Args[0].Name != "prefix" || addrField.Decorators[0].Args[0].Value != "home_" {
		t.Fatalf("unexpected flatten args: %+v", addrField.Decorators[0].Args)
	}

	balanceField := user.Fields[7]
	if len(balanceField.Decorators) != 1 || balanceField.Decorators[0].Name != "encode" || balanceField.Decorators[0].Args[0].Value != "number" {
		t.Fatalf("unexpected encode decorator: %+v", balanceField.Decorators)
	}

	status := f.Enums[0]
	if len(status.Values) != 3 {
		t.Fatalf("expected 3 enum values, got %d", len(status.Values))
	}
	if status.Values[0].Name != "UNSPECIFIED" || len(status.Values[0].Decorators) != 0 {
		t.Fatalf("unexpected first enum value: %+v", status.Values[0])
	}
	if status.Values[1].Name != "ACTIVE" || status.Values[1].Decorators[0].Args[0].Value != "active" {
		t.Fatalf("unexpected second enum value: %+v", status.Values[1])
	}

	event := f.Messages[2]
	if event.Name != "Event" {
		t.Fatalf("expected Event message, got %s", event.Name)
	}
	if len(event.Fields) != 2 {
		t.Fatalf("expected 2 fields on Event, got %d", len(event.Fields))
	}
	payload := event.Fields[1]
	if payload.Name != "payload" || payload.Oneof == nil {
		t.Fatalf("expected payload field to be a oneof: %+v", payload)
	}
	if len(payload.Oneof.Args) != 2 || payload.Oneof.Args[0].Name != "discriminator" || payload.Oneof.Args[0].Value != "type" {
		t.Fatalf("unexpected oneof args: %+v", payload.Oneof.Args)
	}
	if payload.Oneof.Args[1].Name != "flatten" || payload.Oneof.Args[1].Value != "true" {
		t.Fatalf("unexpected oneof flatten arg: %+v", payload.Oneof.Args[1])
	}
	if len(payload.Oneof.Variants) != 2 {
		t.Fatalf("expected 2 oneof variants, got %d", len(payload.Oneof.Variants))
	}
	if payload.Oneof.Variants[0].Name != "text" || payload.Oneof.Variants[0].Type.Name != "TextPayload" {
		t.Fatalf("unexpected first oneof variant: %+v", payload.Oneof.Variants[0])
	}
	if payload.Oneof.Variants[0].Decorators[0].Name != "tag" || payload.Oneof.Variants[0].Decorators[0].Args[0].Value != "text" {
		t.Fatalf("unexpected first oneof variant tag: %+v", payload.Oneof.Variants[0].Decorators)
	}

	getPortfolio := f.Messages[6]
	if getPortfolio.Name != "GetPortfolioRequest" {
		t.Fatalf("expected GetPortfolioRequest, got %s", getPortfolio.Name)
	}
	tf := getPortfolio.Fields[0]
	if len(tf.Decorators) != 2 || tf.Decorators[0].Name != "query" || tf.Decorators[0].Args[0].Value != "timeframe" {
		t.Fatalf("unexpected query decorator: %+v", tf.Decorators)
	}
	if tf.Decorators[1].Name != "required" {
		t.Fatalf("expected required decorator, got %+v", tf.Decorators)
	}
	acctField := getPortfolio.Fields[1]
	if !acctField.Optional {
		t.Fatalf("expected account_id to be optional")
	}
}

func TestParseService(t *testing.T) {
	f, err := Parse(sampleService)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(f.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d: %+v", len(f.Imports), f.Imports)
	}
	if len(f.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(f.Services))
	}
	svc := f.Services[0]
	if svc.BasePath != "/api/v1" {
		t.Fatalf("base path = %q", svc.BasePath)
	}
	if len(svc.Headers) != 1 || svc.Headers[0].Name != "X-API-Key" {
		t.Fatalf("unexpected service headers: %+v", svc.Headers)
	}
	if len(svc.Headers[0].Decorators) != 3 {
		t.Fatalf("unexpected header decorators: %+v", svc.Headers[0].Decorators)
	}
	if svc.Headers[0].Decorators[1].Name != "format" || svc.Headers[0].Decorators[1].Args[0].Value != "uuid" {
		t.Fatalf("unexpected format decorator: %+v", svc.Headers[0].Decorators[1])
	}
	if svc.Headers[0].Decorators[2].Name != "auth" || svc.Headers[0].Decorators[2].Args[0].Value != "api_key" {
		t.Fatalf("unexpected auth decorator: %+v", svc.Headers[0].Decorators[2])
	}
	if len(svc.RPCs) != 5 {
		t.Fatalf("expected 5 rpcs, got %d", len(svc.RPCs))
	}

	create := svc.RPCs[0]
	if create.Name != "createUser" || create.RequestType != "CreateUserRequest" || create.ResponseType != "User" {
		t.Fatalf("unexpected createUser rpc: %+v", create)
	}
	if len(create.Decorators) != 1 || create.Decorators[0].Name != "post" || create.Decorators[0].Args[0].Value != "/users" {
		t.Fatalf("unexpected createUser decorators: %+v", create.Decorators)
	}

	get := svc.RPCs[1]
	if len(get.Headers) != 1 || get.Headers[0].Name != "X-Request-Id" {
		t.Fatalf("unexpected getUser headers: %+v", get.Headers)
	}
	if get.Decorators[0].Name != "get" || get.Decorators[0].Args[0].Value != "/users/{id}" {
		t.Fatalf("unexpected getUser decorators: %+v", get.Decorators)
	}

	update := svc.RPCs[2]
	if len(update.Decorators) != 2 || update.Decorators[0].Name != "put" || update.Decorators[1].Name != "body" {
		t.Fatalf("unexpected updateProfile decorators: %+v", update.Decorators)
	}
	if update.Decorators[1].Args[0].Value != "profile" {
		t.Fatalf("unexpected body decorator arg: %+v", update.Decorators[1])
	}

	stream := svc.RPCs[3]
	if len(stream.Decorators) != 2 || stream.Decorators[0].Name != "get" || stream.Decorators[1].Name != "stream" {
		t.Fatalf("unexpected streamEvents decorators: %+v", stream.Decorators)
	}

	search := svc.RPCs[4]
	if len(search.Decorators) != 1 || search.Decorators[0].Name != "query" || search.Decorators[0].Args[0].Value != "/users/search" {
		t.Fatalf("unexpected searchUsers decorators: %+v", search.Decorators)
	}
}

func TestParseNestedMessageAndEnum(t *testing.T) {
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
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	outer := f.Messages[0]
	if len(outer.Nested) != 1 || outer.Nested[0].Name != "Inner" {
		t.Fatalf("unexpected nested messages: %+v", outer.Nested)
	}
	if len(outer.NestedEn) != 1 || outer.NestedEn[0].Name != "Kind" {
		t.Fatalf("unexpected nested enums: %+v", outer.NestedEn)
	}
}

func TestParseEmptyDecoratorArgs(t *testing.T) {
	src := `
message M {
  x: string @weird()
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	field := f.Messages[0].Fields[0]
	if len(field.Decorators) != 1 || field.Decorators[0].Name != "weird" || field.Decorators[0].Args != nil {
		t.Fatalf("unexpected decorator: %+v", field.Decorators[0])
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing colon after field name", "message M {\n  x string\n}\n"},
		{"missing message name", "message {\n}\n"},
		{"unterminated string", "message M {\n  x: string @format(\"uuid\n}\n"},
		{"unexpected top-level token", "banana\n"},
		{"unclosed brace", "message M {\n  x: string\n"},
		{"bad map syntax", "message M {\n  x: map[string]\n}\n"},
		{"illegal character", "message M {\n  x: string #\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err == nil {
				t.Fatalf("expected parse error for %q, got nil", tc.name)
			}
		})
	}
}

func TestLexerPositions(t *testing.T) {
	lex := NewLexer("message M {\n  x: string\n}")
	var kinds []Kind
	for {
		tok, err := lex.Next()
		if err != nil {
			t.Fatalf("unexpected lex error: %v", err)
		}
		kinds = append(kinds, tok.Kind)
		if tok.Kind == EOF {
			break
		}
	}
	want := []Kind{IDENT, IDENT, LBRACE, IDENT, COLON, IDENT, RBRACE, EOF}
	if len(kinds) != len(want) {
		t.Fatalf("expected %d tokens, got %d: %+v", len(want), len(kinds), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("token %d: expected %s, got %s", i, want[i], kinds[i])
		}
	}
}

func TestLexerComments(t *testing.T) {
	src := `
// line comment
message M { // trailing
  /* block
     comment */
  x: string
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(f.Messages) != 1 || len(f.Messages[0].Fields) != 1 {
		t.Fatalf("unexpected parse result: %+v", f)
	}
}

func TestParseDocComments(t *testing.T) {
	src := `
/// A user of the system.
message User {
  /// Unique identifier.
  id: string
  name: string
}

/// User lifecycle state.
enum Status {
  /// Freshly created, not yet verified.
  ACTIVE
  INACTIVE
}

/// Manages user accounts.
service UserService {
  /// Creates a new user.
  createUser(CreateUserRequest) -> User @post("/users")
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	user := f.Messages[0]
	if user.Doc != "A user of the system." {
		t.Fatalf("unexpected message doc: %q", user.Doc)
	}
	if user.Fields[0].Doc != "Unique identifier." {
		t.Fatalf("unexpected field doc: %q", user.Fields[0].Doc)
	}
	if user.Fields[1].Doc != "" {
		t.Fatalf("expected no doc on name field, got %q", user.Fields[1].Doc)
	}

	status := f.Enums[0]
	if status.Doc != "User lifecycle state." {
		t.Fatalf("unexpected enum doc: %q", status.Doc)
	}
	if status.Values[0].Doc != "Freshly created, not yet verified." {
		t.Fatalf("unexpected enum value doc: %q", status.Values[0].Doc)
	}
	if status.Values[1].Doc != "" {
		t.Fatalf("expected no doc on INACTIVE, got %q", status.Values[1].Doc)
	}

	svc := f.Services[0]
	if svc.Doc != "Manages user accounts." {
		t.Fatalf("unexpected service doc: %q", svc.Doc)
	}
	if svc.RPCs[0].Doc != "Creates a new user." {
		t.Fatalf("unexpected rpc doc: %q", svc.RPCs[0].Doc)
	}
}

func TestParseDocCommentMultiline(t *testing.T) {
	src := `
/// Line one.
/// Line two.
message M {
  x: string
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	want := "Line one.\nLine two."
	if f.Messages[0].Doc != want {
		t.Fatalf("unexpected multiline doc: %q", f.Messages[0].Doc)
	}
}

func TestParseDocCommentResetByPlainComment(t *testing.T) {
	src := `
/// Real doc.
// plain comment breaks the doc block
message M {
  x: string
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if f.Messages[0].Doc != "" {
		t.Fatalf("expected doc to be reset by plain comment, got %q", f.Messages[0].Doc)
	}
}

func TestParseErrorUnionReturnTypes(t *testing.T) {
	src := `
package examples.simpleapi.services

service UserService {
  getUser(GetUserRequest) -> User | NotFoundError | ValidationError @get("/users/{id}")
  createUser(CreateUserRequest) -> User @post("/users")
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	svc := f.Services[0]
	getUser := svc.RPCs[0]
	if getUser.ResponseType != "User" {
		t.Fatalf("expected success type User, got %q", getUser.ResponseType)
	}
	if len(getUser.ErrorTypes) != 2 || getUser.ErrorTypes[0] != "NotFoundError" || getUser.ErrorTypes[1] != "ValidationError" {
		t.Fatalf("unexpected error types: %+v", getUser.ErrorTypes)
	}
	if len(getUser.Decorators) != 1 || getUser.Decorators[0].Name != "get" {
		t.Fatalf("unexpected decorators after error union: %+v", getUser.Decorators)
	}

	createUser := svc.RPCs[1]
	if len(createUser.ErrorTypes) != 0 {
		t.Fatalf("expected no error types on createUser, got %+v", createUser.ErrorTypes)
	}
}

func TestParseMessageLevelDecorator(t *testing.T) {
	src := `
message NotFoundError @status(404) {
  resource_type: string
  resource_id: string
}

message PlainMessage {
  x: string
}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	notFound := f.Messages[0]
	if len(notFound.Decorators) != 1 || notFound.Decorators[0].Name != "status" || notFound.Decorators[0].Args[0].Value != "404" {
		t.Fatalf("unexpected message decorators: %+v", notFound.Decorators)
	}
	plain := f.Messages[1]
	if len(plain.Decorators) != 0 {
		t.Fatalf("expected no decorators on PlainMessage, got %+v", plain.Decorators)
	}
}
