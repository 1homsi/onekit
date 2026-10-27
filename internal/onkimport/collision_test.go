package onkimport

import (
	"strings"
	"testing"
)

func TestImportRenamesFieldsThatCollideAfterSanitizing(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},
"components":{"schemas":{"Pet":{"type":"object","properties":{"user_id":{"type":"string"},"user-id":{"type":"string"},"user.id":{"type":"string"},"userId":{"type":"string"}}}}},
"paths":{"/pets":{"get":{"operationId":"getPet","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	requireFaithfulTypeSpec(t, result)
	for _, want := range []string{"  userId?: string;\n", "  user_id_2?: string;\n", "  user_id_3?: string;\n", "  user_id_4?: string;\n"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
}
