package onkimport

import (
	"strings"
	"testing"
)

func TestImportInfersObjectsAndNullableTypes(t *testing.T) {
	spec := `{"openapi":"3.1.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{
"Pet":{"required":["name","nick"],"properties":{"name":{"type":"string"},"nick":{"type":["string","null"]},"age":{"type":"integer","nullable":true},"tags":{"items":{"type":"string"}}}}}},
"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"name: string\n", "nick: string?", "age: int32?", "tags: string[]"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s\n%v", want, src, result.Warnings)
		}
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "unsupported composition") {
			t.Fatalf("schema degraded to json: %v", result.Warnings)
		}
	}
}
