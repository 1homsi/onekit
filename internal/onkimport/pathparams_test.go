package onkimport

import (
	"strings"
	"testing"
)

func TestImportRewritesRenamedAndOptionalPathParameters(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets/{pet-id}/toys/{toyId}":{"get":{"operationId":"getToy","parameters":[{"name":"pet-id","in":"path","required":true,"schema":{"type":"string"}},{"name":"toyId","in":"path","schema":{"type":"string"}}],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, `/pets/{pet_id}/toys/{toy_id}`) && !strings.Contains(src, `/pets/{pet_id}/toys/{toyId}`) {
		t.Fatalf("route not rewritten:\n%s", src)
	}
	requireFaithfulTypeSpec(t, result)
}
