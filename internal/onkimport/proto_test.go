package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const todoProto = `// Todo API.
syntax = "proto3";

package example.todos.v1;

import "google/api/annotations.proto";
import "google/protobuf/timestamp.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/wrappers.proto";
import "google/protobuf/struct.proto";

option go_package = "example.com/todos/v1;todos";

// Whether a todo is finished.
enum Status {
  STATUS_UNSPECIFIED = 0;
  // Still to do.
  OPEN = 1;
  DONE = 2 [deprecated = true];
}

// A single todo.
message Todo {
  message Label {
    string name = 1;
    int32 weight = 2;
  }
  enum Priority {
    LOW = 0;
    HIGH = 1;
  }

  // Server-generated id.
  string id = 1;
  string title = 2;
  Status status = 3;
  google.protobuf.Timestamp created_at = 4;
  repeated string tags = 5;
  map<string, string> attributes = 6;
  optional int64 estimate_minutes = 7;
  repeated Label labels = 8;
  Priority priority = 9;
  bytes payload = 10;
  uint64 version = 11 [deprecated = true];
  google.protobuf.StringValue note = 12;
  google.protobuf.Struct extra = 13;
  oneof assignee {
    string user = 14;
    Label team = 15;
  }
  reserved 20, 21;
  reserved "old_name";
}

message ListTodosRequest {
  string filter = 1;
  int32 page_size = 2;
  repeated string tags = 3;
}

message ListTodosResponse {
  repeated Todo todos = 1;
}

message GetTodoRequest {
  string id = 1;
}

message CreateTodoRequest {
  Todo todo = 1;
}

message UpdateTodoRequest {
  string id = 1;
  Todo todo = 2;
}

service TodoService {
  // List todos.
  rpc ListTodos(ListTodosRequest) returns (ListTodosResponse) {
    option (google.api.http) = {
      get: "/v1/todos"
    };
  }
  rpc GetTodo(GetTodoRequest) returns (Todo) {
    option (google.api.http) = { get: "/v1/todos/{id=todos/*}" };
  }
  rpc CreateTodo(CreateTodoRequest) returns (Todo) {
    option (google.api.http) = {
      post: "/v1/todos"
      body: "todo"
      additional_bindings { post: "/v1/other" body: "*" }
    };
  }
  rpc UpdateTodo(UpdateTodoRequest) returns (Todo) {
    option (google.api.http) = { patch: "/v1/todos/{id}" body: "todo" };
  }
  rpc DeleteTodo(GetTodoRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { delete: "/v1/todos/{id}" };
  }
  rpc WatchTodos(ListTodosRequest) returns (stream Todo) {
    option (google.api.http) = { post: "/v1/todos:watch" body: "*" };
  }
  rpc Sync(stream Todo) returns (stream Todo);
  rpc Legacy(GetTodoRequest) returns (Todo) {
    option deprecated = true;
  }
}
`

func importProto(t *testing.T, src string) (*Result, string) {
	t.Helper()
	result, err := ImportProto([]byte(src), Options{})
	if err != nil {
		t.Fatalf("ImportProto: %v", err)
	}
	ast, err := onklang.Parse(string(result.Source))
	if err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, result.Source)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}}); err != nil {
		t.Fatalf("output does not pass onek check: %v\n%s", err, result.Source)
	}
	return result, string(result.Source)
}

func TestImportProtoConvertsAnAPIShapedFile(t *testing.T) {
	result, src := importProto(t, todoProto)
	for _, want := range []string{
		"package example.todos.v1",
		"/// A single todo.",
		"message Todo {",
		"  /// Server-generated id.",
		"id: string",
		"status: Status",
		"created_at: timestamp",
		"tags: string[]",
		"attributes: map[string, string]",
		"estimate_minutes: int64?",
		"labels: TodoLabel[]",
		"priority: TodoPriority",
		"payload: bytes",
		"version: uint64 @deprecated",
		"note: string?",
		"extra: json",
		`assignee: oneof(discriminator: "type")`,
		`user: string @tag("user")`,
		`team: TodoLabel @tag("team")`,
		"message TodoLabel {",
		"enum TodoPriority {",
		"enum Status {",
		"/// Still to do.",
		"service TodoService {",
		"/// List todos.",
		`listTodos(ListTodosRequest) -> ListTodosResponse @get("/v1/todos")`,
		`getTodo(GetTodoRequest) -> Todo @get("/v1/todos/{id}")`,
		`createTodo(CreateTodoRequest) -> Todo @body("todo") @post("/v1/todos")`,
		`updateTodo(UpdateTodoRequest) -> Todo @body("todo") @patch("/v1/todos/{id}")`,
		`deleteTodo(GetTodoRequest) -> Empty @delete("/v1/todos/{id}")`,
		`watchTodos(ListTodosRequest) -> Todo @get("/v1/todos:watch") @stream`,
		`legacy(GetTodoRequest) -> Todo @post("/example.todos.v1.TodoService/Legacy") @deprecated`,
		"message Empty {",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q in:\n%s", want, src)
		}
	}
	if strings.Contains(src, "sync(") {
		t.Fatalf("a bidirectional streaming RPC has no HTTP mapping:\n%s", src)
	}
	for _, want := range []string{"limit", "Sync"} {
		_ = want
	}
	warnings := strings.Join(result.Warnings, "\n")
	for _, want := range []string{
		"oneofs use onekit's discriminated encoding",
		`path template "{id=todos/*}" lost its pattern`,
		"client and bidirectional streaming",
		"server streaming needs a GET binding",
	} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("missing warning %q in:\n%s", want, warnings)
		}
	}
}

func messageBody(t *testing.T, src, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(src, "message "+name+" {")
	if !ok {
		t.Fatalf("message %s not found in:\n%s", name, src)
	}
	body, _, _ := strings.Cut(rest, "}")
	return body
}

func TestImportProtoMarksQueryFieldsForNonBodyBindings(t *testing.T) {
	_, src := importProto(t, todoProto)
	section := messageBody(t, src, "ListTodosRequest")
	for _, want := range []string{"filter: string @query", "page_size: int32 @query", "tags: string[] @query"} {
		if !strings.Contains(section, want) {
			t.Fatalf("missing %q in:\n%s", want, section)
		}
	}
	get := messageBody(t, src, "GetTodoRequest")
	if strings.Contains(get, "@query") {
		t.Fatalf("path-bound fields are not query parameters:\n%s", get)
	}
}

func TestImportProtoHonoursMapOfMessagesEnumsAndScoping(t *testing.T) {
	_, src := importProto(t, `syntax = "proto3";
package shop;
message Item { string sku = 1; }
message Cart {
  map<string, Item> items = 1;
  Kind kind = 2;
  enum Kind { SMALL = 0; LARGE = 1; }
  Outer.Inner nested = 3;
}
message Outer { message Inner { int32 n = 1; } }
enum Kind { OTHER = 0; }
`)
	for _, want := range []string{"items: map[string, Item]", "kind: CartKind", "nested: OuterInner", "enum Kind {", "enum CartKind {"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q in:\n%s", want, src)
		}
	}
}

func TestImportProtoReportsWhatItCannotExpress(t *testing.T) {
	result, src := importProto(t, `syntax = "proto3";
package p;
import "other/types.proto";
message M { other.Thing thing = 1; google.protobuf.Duration wait = 2; map<int32, string> by_id = 3; }
service S {
  rpc Nested(M) returns (M) { option (google.api.http) = { get: "/v1/{thing.id}" }; }
  rpc Missing(Unknown) returns (M);
}
`)
	if !strings.Contains(src, "thing: json") || !strings.Contains(src, "wait: string") || !strings.Contains(src, "by_id: map[string, string]") {
		t.Fatalf("fields were not degraded as documented:\n%s", src)
	}
	if strings.Contains(src, "rpc") || strings.Contains(src, "nested(") || strings.Contains(src, "missing(") {
		t.Fatalf("unmappable RPCs must be skipped, not half-emitted:\n%s", src)
	}
	warnings := strings.Join(result.Warnings, "\n")
	for _, want := range []string{
		`import "other/types.proto" is not followed`,
		`type "other.Thing" is not declared in this file`,
		"google.protobuf.Duration has no onekit equivalent",
		`path parameter "thing.id" is not a top-level scalar field`,
		`request type "Unknown" is not a message declared in this file`,
	} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("missing warning %q in:\n%s", want, warnings)
		}
	}
}

func TestImportProtoRejectsBrokenInput(t *testing.T) {
	for src, want := range map[string]string{
		`syntax = "proto3"; message M { string a = ; }`:        "expected a field number",
		`syntax = "proto3"; message M { string a = 1;`:         "unterminated message M",
		`syntax = "proto3"; message M { group G = 1 {} }`:      "groups are not supported",
		`syntax = "proto3"; service S { rpc A(B) returns C; }`: `expected "("`,
		`syntax = "proto3";`:                                   "declares no messages",
		`syntax = "proto3"; message M { string a = 1; } /* x`:  "unterminated block comment",
	} {
		_, err := ImportProto([]byte(src), Options{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: want error containing %q, got %v", src, want, err)
		}
	}
}

func TestImportProtoUsesThePackageOverrideAndIsDeterministic(t *testing.T) {
	first, err := ImportProto([]byte(todoProto), Options{Package: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportProto([]byte(todoProto), Options{Package: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.Source), "package custom") || first.Package != "custom" {
		t.Fatalf("package override ignored:\n%s", first.Source)
	}
	if string(first.Source) != string(second.Source) || strings.Join(first.Warnings, "|") != strings.Join(second.Warnings, "|") {
		t.Fatal("imports must be deterministic")
	}
}

func TestImportProtoWarnsOnlyWhenSharedRequestsReallyConflict(t *testing.T) {
	result, _ := importProto(t, `syntax = "proto3";
package p;
message Ref { string id = 1; string note = 2; }
message Out { string v = 1; }
service S {
  rpc A(Ref) returns (Out) { option (google.api.http) = { get: "/a/{id}" }; }
  rpc B(Ref) returns (Out) { option (google.api.http) = { delete: "/b/{id}" }; }
  rpc C(Ref) returns (Out) { option (google.api.http) = { get: "/c" }; }
}
`)
	warnings := strings.Join(result.Warnings, "\n")
	if strings.Count(warnings, "is shared by RPCs with different HTTP bindings") != 1 || !strings.Contains(warnings, "S.C:") {
		t.Fatalf("only C binds different path fields than the first use:\n%s", warnings)
	}
}
