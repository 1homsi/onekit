package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsRequiredFieldsTheRouteNeverSends(t *testing.T) {
	tests := map[string]string{
		"get": `
message Q { id: string  note: string @required }
service API { a(Q) -> Q @get("/x/{id}") }
`,
		"body": `
message B { v: string }
message Q { b: B  note: string @required }
service API { a(Q) -> Q @post("/x") @body("b") }
`,
	}
	for name, schema := range tests {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), `@required field "note" on RPC a is never sent`) {
			t.Fatalf("%s: want unbound required error, got %v", name, err)
		}
	}
	for _, schema := range []string{
		`message Q { id: string @required  page: int32? @query @required }
service API { a(Q) -> Q @get("/x/{id}") }`,
		`message Q { note: string @required }
service API { a(Q) -> Q @post("/x") }`,
	} {
		if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}}); err != nil {
			t.Fatalf("bound required fields rejected: %v", err)
		}
	}
}
