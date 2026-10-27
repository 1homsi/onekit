package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onktest"
)

func TestOneofAcceptsRequiredOnly(t *testing.T) {
	pkg, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model A {
  v: string;
}

union MP {
  a: A,
}

model M {
  @Onkt.required
  p: MP;
}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.Files[0].Messages[1].Fields[0].HasDecorator("required") {
		t.Fatal("@required dropped from oneof field")
	}
}

// TypeSpec refuses a length bound on a union property; a document carrying
// one on a oneof is refused by the compiler.
func TestOneofRejectsOtherDecorators(t *testing.T) {
	n := onktest.N
	oneof := onktest.Oneof("p", nil, onktest.Variant("a", "A"))
	oneof.Decorators = append(oneof.Decorators, onktest.Dec("len", n("1"), n("2")))
	_, err := Compile([]Source{{Path: "api.tsp", AST: onktest.File("", onktest.Message("A"), onktest.Message("M", oneof))}})
	if err == nil || !strings.Contains(err.Error(), "only @required is") {
		t.Fatalf("want oneof decorator error, got %v", err)
	}
}
