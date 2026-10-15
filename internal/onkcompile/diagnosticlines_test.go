package onkcompile

import (
	"errors"
	"testing"
)

func TestContractDiagnosticsCarryTheDeclarationLine(t *testing.T) {
	cases := []struct {
		name, schema string
		line         int
	}{
		{"missing path field", "package x\nmessage A { s: string }\nservice S {\n  op(A) -> A @get(\"/a/{id}\")\n}\n", 4},
		{"field never sent", "package x\nmessage A { s: string  b: string[] @required }\nmessage E {}\nservice S {\n  a(A) -> E @get(\"/a\")\n}\n", 5},
		{"path and query", "package x\nmessage A { id: string @query }\nservice S {\n  op(A) -> A @get(\"/a/{id}\")\n}\n", 4},
		{"unknown body field", "package x\nmessage A { s: string }\nservice S {\n  op(A) -> A @post(\"/a\") @body(\"zz\")\n}\n", 4},
		{"unsupported encode", "package x\nmessage A {\n  s: string\n  d: int32 @encode(\"date\")\n}\nmessage E {}\nservice S {\n  a(A) -> E @post(\"/a\")\n}\n", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, tc.schema)}}, CompileOptions{})
			var compileErr *Error
			if !errors.As(err, &compileErr) {
				t.Fatalf("want a compile error, got %v", err)
			}
			if compileErr.Line != tc.line {
				t.Fatalf("line %d, want %d: %v", compileErr.Line, tc.line, err)
			}
		})
	}
}
