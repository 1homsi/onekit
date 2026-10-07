package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

func TestWildcardPathValidation(t *testing.T) {
	cases := []struct {
		name    string
		service string
		want    string
	}{
		{"last segment accepted", `service S { read(Req) -> Res @get("/files/{path...}") }`, ""},
		{"middle segment rejected", `service S { read(Req) -> Res @get("/files/{path...}/x") }`, "must be the last segment"},
		{"partial segment rejected", `service S { read(Req) -> Res @get("/files/a{path...}") }`, "whole segment"},
		{"non-string field rejected", `service S { read(NReq) -> Res @get("/files/{n...}") }`, "requires a string request field"},
		{"websocket rejected", `service S { open(Req) -> Res @ws("/files/{path...}") }`, "wildcard"},
		{"empty route without base path rejected", `service S { read(Empty) -> Res @get("") }`, "base_path"},
		{"empty route with base path accepted", `service S { base_path: "/files"
  read(Empty) -> Res @get("") }`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "package api\nmessage Req { path: string }\nmessage NReq { n: int32 }\nmessage Empty {}\nmessage Res { ok: bool }\n"+tc.service+"\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestMaxBodyDecoratorValidation(t *testing.T) {
	cases := []struct {
		name string
		rpc  string
		want string
	}{
		{"mebibytes accepted", `@post("/x") @max_body("64MiB")`, ""},
		{"byte count accepted", `@post("/x") @max_body("1048576")`, ""},
		{"bad size rejected", `@post("/x") @max_body("lots")`, "positive byte count"},
		{"zero rejected", `@post("/x") @max_body("0")`, "positive byte count"},
		{"get rejected", `@get("/x") @max_body("1MiB")`, "body-bearing"},
		{"ws rejected", `@ws("/x") @max_body("1MiB")`, "@max_body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "package api\nmessage Req { a: string }\nmessage Res { ok: bool }\nservice S { run(Req) -> Res "+tc.rpc+" }\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func compileRulesPackage(t *testing.T, src string) (*onkir.Package, error) {
	t.Helper()
	return Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, src)}})
}

func TestBodylessScalarRequestFieldsBindToTheQueryString(t *testing.T) {
	pkg, err := compileRulesPackage(t, "package api\nmessage Req { id: string  filter: string  limit: int32 }\nmessage Res { ok: bool }\nservice S { read(Req) -> Res @get(\"/x/{id}\") }\n")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]*onkir.Field{}
	for _, field := range pkg.Files[0].Messages[0].Fields {
		fields[field.Name] = field
	}
	if !fields["filter"].HasDecorator("query") || !fields["limit"].HasDecorator("query") {
		t.Fatal("scalar fields that are not path parameters must bind to the query string on a GET")
	}
	if fields["id"].HasDecorator("query") {
		t.Fatal("a path parameter must stay a path parameter")
	}
}

func TestBodylessRequestFieldsThatCannotBindAreRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"message field", "message Inner { a: string }\nmessage Req { inner: Inner }\nmessage Res { ok: bool }\nservice S { read(Req) -> Res @get(\"/x\") }"},
		{"repeated field", "message Req { tags: string[] }\nmessage Res { ok: bool }\nservice S { read(Req) -> Res @delete(\"/x\") }"},
		{"enum field", "enum Kind { A B }\nmessage Req { kind: Kind }\nmessage Res { ok: bool }\nservice S { read(Req) -> Res @get(\"/x\") }"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileRulesPackage(t, "package api\n"+tc.src+"\n")
			if err == nil || !strings.Contains(err.Error(), "never sent") || !strings.Contains(err.Error(), "@query") {
				t.Fatalf("error = %v, want a never-sent error that mentions @query", err)
			}
		})
	}
}

func TestResourceMessageSharedWithABodyVerbIsAccepted(t *testing.T) {
	_, err := compileRulesPackage(t, "package api\nmessage Note { id: string  text: string }\nservice S { get(Note) -> Note @get(\"/notes/{id}\")\n update(Note) -> Note @put(\"/notes/{id}\") }\n")
	if err != nil {
		t.Fatalf("a resource message used by a GET and a PUT is a common shape: %v", err)
	}
}

func TestGuardPlaceholdersMustBePathParameters(t *testing.T) {
	cases := []struct {
		name string
		rpc  string
		want string
	}{
		{"path placeholder accepted", `get(Req) -> Res @get("/x/{id}") @guard("obj/read/:id", "audit")`, ""},
		{"unknown placeholder rejected", `get(Req) -> Res @get("/x/{id}") @guard("obj/read/:other")`, "not a path parameter"},
		{"empty guard rejected", `get(Req) -> Res @get("/x/{id}") @guard`, "at least one pattern"},
		{"websocket rejected", `get(Req) -> Res @ws("/x") @guard("a")`, "@guard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "package api\nmessage Req { id: string }\nmessage Res { ok: bool }\nservice S { "+tc.rpc+" }\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
