package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onklang"
)

func TestOneofAcceptsRequiredOnly(t *testing.T) {
	src := "message A { v: string }\nmessage M {\n  p: oneof {\n    a: A\n  } @required\n}\n"
	pkg, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, src)}})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.Files[0].Messages[1].Fields[0].HasDecorator("required") {
		t.Fatal("@required dropped from oneof field")
	}
	formatted, err := onklang.Format(src)
	if err != nil || !strings.Contains(string(formatted), "} @required") {
		t.Fatalf("format lost the decorator: %v\n%s", err, formatted)
	}
	_, err = Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, "message A {}\nmessage M { p: oneof { a: A } @len(1, 2) }\n")}})
	if err == nil || !strings.Contains(err.Error(), "only @required is") {
		t.Fatalf("want oneof decorator error, got %v", err)
	}
}
