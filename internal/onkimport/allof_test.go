package onkimport

import (
	"strings"
	"testing"
)

func TestImportResolvesSingleAndNestedAllOf(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{
"Status":{"type":"string","enum":["active","gone"]},
"Base":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}},
"Named":{"allOf":[{"$ref":"#/components/schemas/Base"},{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}]},
"Pet":{"allOf":[{"$ref":"#/components/schemas/Named"}],"properties":{"status":{"allOf":[{"$ref":"#/components/schemas/Status"}]}}}}},
"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"id: string\n", "name: string\n", "status: StatusValues?"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s\n%v", want, src, result.Warnings)
		}
	}
}
