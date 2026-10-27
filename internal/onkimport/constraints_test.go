package onkimport

import (
	"strings"
	"testing"
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
	for _, want := range []string{"/** Display name. */\n  @minLength(1)\n  @maxLength(40)\n  name: string;", "  @minValue(0)\n  @maxValue(30)\n  age?: int32;", "  @minValueExclusive(0)\n  weight?: float64;", "  @minItems(1)\n  @maxItems(5)\n  tags: string[];"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	requireFaithfulTypeSpec(t, result)
}
