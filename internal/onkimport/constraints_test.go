package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportCarriesConstraintsAndDescriptions(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{"Pet":{"type":"object","required":["name"],"properties":{
"name":{"type":"string","description":"Display name.","minLength":1,"maxLength":40},
"age":{"type":"integer","minimum":0,"maximum":30},
"weight":{"type":"number","exclusiveMinimum":0},
"tags":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":5}}}}},
"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"/// Display name.\n  name: string @len(1, 40)", "age: int32? @gte(0) @lte(30)", "weight: float64? @gt(0)", "tags: string[] @min_items(1) @max_items(5)"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
}
