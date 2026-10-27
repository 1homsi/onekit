package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsRequiredFieldsTheRouteNeverSends(t *testing.T) {
	// Beside a @body field, a field that is neither a path nor a query
	// parameter does not cross the wire.
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model B { v: string; }
model Q { @body b: B; @Onkt.required @bodyIgnore note: string; }
interface API { @post @route("/x") a(...Q): Q; }
`)}})
	if err == nil || !strings.Contains(err.Error(), `@required field "note" on RPC a is never sent`) {
		t.Fatalf("want unbound required error, got %v", err)
	}
	for _, schema := range []string{
		`model Q { id: string; @Onkt.required note: string; }
interface API { @get @route("/x/{id}") a(...Q): Q; }`,
		`model Q { @Onkt.required id: string; @query @Onkt.required page?: int32; }
interface API { @get @route("/x/{id}") a(...Q): Q; }`,
		`model Q { @Onkt.required note: string; }
interface API { @post @route("/x") a(...Q): Q; }`,
	} {
		if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, schema)}}); err != nil {
			t.Fatalf("bound required fields rejected: %v\n%s", err, schema)
		}
	}
}
