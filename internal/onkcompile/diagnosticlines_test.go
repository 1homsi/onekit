package onkcompile

import (
	"errors"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

// A contract diagnostic carries the line of the declaration it is about:
// the method for a route problem, the field for a field problem.
func TestContractDiagnosticsCarryTheDeclarationLine(t *testing.T) {
	cases := []struct {
		name, schema string
		line         int
	}{
		{"field never sent", `namespace x;

model A {
  s: string;
  @Onkt.required
  b: string[];
}

model E {}

interface S {
  @get
  @route("/a")
  a(...A): E;
}
`, 14},
		{"unsupported encode", `namespace x;

model A {
  s: string;
  @Onkt.encode("date")
  d: int32;
}

model E {}

interface S {
  @post
  @route("/a")
  a(...A): E;
}
`, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, tc.schema)}}, CompileOptions{})
			requireCompileErrorLine(t, err, tc.line)
		})
	}
}

// The route problems TypeSpec's HTTP library already rejects, as documents
// from another frontend may still carry them.
func TestContractDiagnosticsCarryTheMethodLineInDocuments(t *testing.T) {
	q, dec, field, message := onktest.Q, onktest.Dec, onktest.Field, onktest.Message
	method := func(decorators ...onkschema.Decorator) *onkschema.RPCDecl {
		rpc := onktest.RPC("op", "A", "A", decorators...)
		rpc.Line = 4
		return rpc
	}
	cases := []struct {
		name string
		file *onkschema.File
	}{
		{"missing path field", onktest.File("x", message("A", field("s", "string")), onktest.Service("S", "", method(dec("get", q("/a/{id}")))))},
		{"path and query", onktest.File("x", message("A", field("id", "string", dec("query"))), onktest.Service("S", "", method(dec("get", q("/a/{id}")))))},
		{"unknown body field", onktest.File("x", message("A", field("s", "string")), onktest.Service("S", "", method(dec("post", q("/a")), dec("body", q("zz")))))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: tc.file}}, CompileOptions{})
			requireCompileErrorLine(t, err, 4)
		})
	}
}

func requireCompileErrorLine(t *testing.T, err error, line int) {
	t.Helper()
	var compileErr *Error
	if !errors.As(err, &compileErr) {
		t.Fatalf("want a compile error, got %v", err)
	}
	if compileErr.Line != line {
		t.Fatalf("line %d, want %d: %v", compileErr.Line, line, err)
	}
}
