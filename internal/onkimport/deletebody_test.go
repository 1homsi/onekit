package onkimport

import (
	"strings"
	"testing"
)

func TestImportDropsBodiesOnNonBodyVerbs(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets/{id}":{"delete":{"operationId":"removePet","parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"reason":{"type":"string"}}}}}},"responses":{"204":{"description":"gone"}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if strings.Contains(src, "@body") {
		t.Fatalf("body kept on DELETE:\n%s", src)
	}
	requireFaithfulTypeSpec(t, result)
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, "\n"), "request body on DELETE") {
		t.Fatalf("missing warning: %v", result.Warnings)
	}
}
