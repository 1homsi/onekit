package gents

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestTSValidatesIntegerMapKeys(t *testing.T) {
	ast, err := onklang.Parse("package app\nmessage R { by_id: map[int64, string]  by_n: map[uint32, string]  by_s: map[string, string] }\n")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{})
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
