package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsFlattenCycles(t *testing.T) {
	for schema, want := range map[string]string{
		`message Node { child: Node? @flatten(prefix: "c_") }`: "@flatten cycle: Node -> Node",
		`message A { b: B @flatten(prefix: "b_") }
message B { a: A @flatten(prefix: "a_") }`: "@flatten cycle: A -> B -> A",
	} {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message Addr { city: string }
message Order { bill: Addr @flatten(prefix: "bill_")  ship: Addr @flatten(prefix: "ship_") }`)}}); err != nil {
		t.Fatalf("acyclic flatten rejected: %v", err)
	}
}
