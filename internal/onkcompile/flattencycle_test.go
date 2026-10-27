package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsFlattenCycles(t *testing.T) {
	for schema, want := range map[string]string{
		`
model Node {
  @Onkt.flatten("c_")
  child?: Node;
}
`: "@flatten cycle: Node -> Node",
		`
model A {
  @Onkt.flatten("b_")
  b: B;
}

model B {
  @Onkt.flatten("a_")
  a: A;
}
`: "@flatten cycle: A -> B -> A",
	} {
		_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model Addr {
  city: string;
}

model Order {
  @Onkt.flatten("bill_")
  bill: Addr;
  @Onkt.flatten("ship_")
  ship: Addr;
}
`)}}); err != nil {
		t.Fatalf("acyclic flatten rejected: %v", err)
	}
}
