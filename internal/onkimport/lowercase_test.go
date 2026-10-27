package onkimport

import (
	"strings"
	"testing"
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
	requireFaithfulTypeSpec(t, result)
	if !strings.Contains(src, "model Pet {") {
		t.Fatalf("component not capitalized:\n%s", src)
	}
}
