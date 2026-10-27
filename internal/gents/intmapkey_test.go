package gents

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestTSValidatesIntegerMapKeys(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model R {
  @Onkt.mapKey("int64")
  by_id: Record<string>;
  @Onkt.mapKey("uint32")
  by_n: Record<string>;
  by_s: Record<string>;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	types := string(GenerateTypes(pkg.Files[0]))
	for _, want := range []string{`/^-?[0-9]+$/.test(k)`, `/^[0-9]+$/.test(k)`} {
		if !strings.Contains(types, want) {
			t.Errorf("missing key check %s in:\n%s", want, types)
		}
	}
	if strings.Count(types, "Object.keys(") != 2 {
		t.Errorf("string keys need no check:\n%s", types)
	}
}
