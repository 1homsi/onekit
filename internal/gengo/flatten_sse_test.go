package gengo

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

func genFile(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onktest.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "schema.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

// mustParseGo asserts the emitted Go is at least syntactically valid, which
// catches duplicate `:=` declarations and unused type-switch bindings.
func mustParseGo(t *testing.T, label string, data []byte) {
	t.Helper()
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, label+".go", data, parser.AllErrors); err != nil {
		t.Fatalf("%s emitted invalid Go: %v\n%s", label, err, data)
	}
}

// writeFlattenUnmarshalAssignments emitted `childRaw :=` / `hasChild :=` at
// function scope once per flattened field, so a second @flatten in the same
// message redeclared both and the package did not compile.
const twoFlattens = `namespace p;

model A {
  a: string;
}

model B {
  b: string;
}

model Two {
  @Onkt.flatten
  x: A;
  @Onkt.flatten
  y: B;
}
`

func TestTwoFlattenFieldsInOneMessageEmitValidGo(t *testing.T) {
	out, err := GenerateTypes(genFile(t, twoFlattens))
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	src := string(out)

	// Each flattened field may declare its own `childRaw`/`hasChild`, but
	// only inside a block - two un-scoped `:=` at function scope is the
	// regression. Validity is asserted via go/parser below.
	// The second flattened field must still be unmarshalled into its own field.
	if strings.Count(src, "if hasChild {") != 2 {
		t.Errorf("expected both flattened fields to be unmarshalled:\n%s", src)
	}
	if !strings.Contains(src, "m.X = child") || !strings.Contains(src, "m.Y = child") {
		t.Errorf("expected both flattened fields to be assigned:\n%s", src)
	}
	mustParseGo(t, "types", out)
}

// A `@stream` method with no error union emitted `switch e := err.(type)` with
// only a `default:` arm, which Go rejects: the binding is used by no case.
// Guarded by go/parser, so any future rewrite that reintroduces an unused
// binding fails here regardless of its exact shape.
const streamWithoutErrorUnion = `namespace p;

model Req {
  room: string;
}

model Tick {
  id: string;
}

@route("/v1")
interface S {
  @get
  @route("/events/{room}")
  @Onkt.stream
  watch(...Req): Tick;
}
`

func TestStreamWithoutErrorUnionEmitsValidGo(t *testing.T) {
	out, err := GenerateServer(genFile(t, streamWithoutErrorUnion))
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	src := string(out)

	_ = src
	mustParseGo(t, "server", out)
}

// Non-regression: a stream method WITH an error union must keep its typed arms.
const streamWithErrorUnion = `namespace p;

model Req {
  room: string;
}

model Tick {
  id: string;
}

@error
model Boom {
  @statusCode _: 500;
  message: string;
}

@route("/v1")
interface S {
  @get
  @route("/events/{room}")
  @Onkt.stream
  watch(...Req): Tick | Boom;
}
`

func TestStreamWithErrorUnionKeepsTypedArms(t *testing.T) {
	out, err := GenerateServer(genFile(t, streamWithErrorUnion))
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	src := string(out)

	if !strings.Contains(src, `SendWithEvent("error", streamErr0)`) {
		t.Errorf("expected the typed error arm to send the error value:\n%s", src)
	}
	mustParseGo(t, "server", out)
}
