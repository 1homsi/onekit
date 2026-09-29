package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compileFile(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "schema.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

// A `json` field is an arbitrary JSON value, so `false`, `0`, `""`, `[]` and
// `{}` are real payloads. gengo models this as json.RawMessage, whose
// `omitempty` drops only len == 0 — so RawMessage("0") IS transmitted. A
// Python truthiness test silently dropped all of them, and because every
// decoder defaults an absent field, the loss was invisible at every hop.
const jsonFieldSchema = `package p
message Res { j: json }
`

func TestJSONFieldEmitsIsNotNoneGuard(t *testing.T) {
	src := string(GenerateTypes(compileFile(t, jsonFieldSchema)))

	if !strings.Contains(src, "if self.j is not None:") {
		t.Errorf("json field must use an `is not None` guard so falsy values survive; got:\n%s", src)
	}
	if strings.Contains(src, "if self.j:") {
		t.Errorf("json field must not use a truthiness guard; got:\n%s", src)
	}
}

// Other scalar types keep the documented omitempty-mirroring behavior, so this
// is a non-regression guard against widening the fix too far.
const scalarFieldSchema = `package p
message Res {
  s: string
  n: int32
  b: bool
  o: string?
}
`

func TestScalarFieldsKeepOmitemptyMirroring(t *testing.T) {
	src := string(GenerateTypes(compileFile(t, scalarFieldSchema)))

	for _, name := range []string{"self.s", "self.n", "self.b"} {
		if !strings.Contains(src, "if "+name+":") {
			t.Errorf("scalar %q should keep the truthiness guard; got:\n%s", name, src)
		}
	}
	if !strings.Contains(src, "if self.o is not None:") {
		t.Errorf("optional scalar should use `is not None`; got:\n%s", src)
	}
}
