package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func openAPIForSchema(t *testing.T, schema string) string {
	t.Helper()
	ast, err := onktest.Parse(schema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.tsp", AST: ast}})
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
	doc := openAPIForSchema(t, `namespace app;

model Counters {
  small: uint32;
  @encode(string)
  big: uint64;
  @encode(string)
  signed: int64;
  plain: uint64;
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
