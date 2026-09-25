package onkimport

import (
	"strings"
	"testing"
)

func TestImportLengthAndItemBounds(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets":{"post":{"operationId":"createPet",
"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{
  "nick":{"type":"string","minLength":3},
  "code":{"type":"string","minLength":5,"maxLength":2},
  "tags":{"type":"array","items":{"type":"string"},"minItems":4,"maxItems":1}
}}}}},"responses":{"200":{"description":"ok"}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, "nick: string? @len(3, 2147483647)") || strings.Contains(src, "@len(5, 2)") || strings.Contains(src, "@min_items(4)") {
		t.Fatalf("unexpected constraints:\n%s", src)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, "minLength 5 is greater than maxLength 2") || !strings.Contains(warnings, "minItems 4 is greater than maxItems 1") {
		t.Fatalf("warnings = %s", warnings)
	}
}
