package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportCapitalizesLowercaseComponentNames(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"pets","version":"1"},
"components":{"schemas":{"pet":{"type":"object","properties":{"name":{"type":"string"}}}}},
"paths":{"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/pet"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
	if !strings.Contains(src, "message Pet {") {
		t.Fatalf("component not capitalized:\n%s", src)
	}
}
