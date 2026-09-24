package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportKeepsEveryStatusOfASharedErrorSchema(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{"Error":{"type":"object","properties":{"message":{"type":"string"}}}}},
"paths":{"/pets":{"get":{"operationId":"listPets","responses":{
"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}},
"400":{"description":"bad","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}},
"404":{"description":"missing","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}},
"5XX":{"description":"server","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"message Error @status(400)", "message Error404 @status(404)", "message Error500 @status(500)", "| Error | Error404 | Error500"} {
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
