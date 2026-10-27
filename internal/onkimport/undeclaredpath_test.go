package onkimport

import (
	"strings"
	"testing"
)

func TestImportDeclaresUndeclaredPathParameters(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/owners/{owner-id}/pets/{petId}":{"get":{"operationId":"getPet",
"parameters":[{"name":"petId","in":"path","required":true,"schema":{"type":"integer"}}],"responses":{"200":{"description":"ok"}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	requireFaithfulTypeSpec(t, result)
	if !strings.Contains(src, "  owner_id: string;\n") || !strings.Contains(src, "  @get\n  @route(\"/owners/{owner_id}/pets/{petId}\")\n") {
		t.Fatalf("undeclared parameter not imported:\n%s", src)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), `path parameter "owner-id" is not declared`) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}
