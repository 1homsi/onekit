package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func openAPIForSchema(t *testing.T, schema string) string {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(pkg.Files[0], Options{Title: "T", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOpenAPIDescribesUnsignedAndStringIntegers(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message Counters {
  small: uint32
  big: uint64
  signed: int64
  plain: uint64 @encode(number)
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"small: type: integer maximum: 4294967295 minimum: 0 format: int64",
		"big: type: string pattern: ^[0-9]+$ format: uint64",
		"signed: type: string pattern: ^-?[0-9]+$ format: int64",
		"plain: type: integer minimum: 0 format: uint64",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
