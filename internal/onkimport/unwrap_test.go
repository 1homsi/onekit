package onkimport

import (
	"strings"
	"testing"
)

func TestImportUnwrapsNonObjectResponses(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{
"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"array","items":{"type":"string"}}}}}}}},
"/count":{"get":{"operationId":"countPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"integer"}}}}}}},
"/tags":{"get":{"operationId":"tagCounts","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","additionalProperties":{"type":"integer"}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if strings.Count(src, "@Onkt.unwrap") != 3 {
		t.Fatalf("responses not unwrapped:\n%s", src)
	}
	requireFaithfulTypeSpec(t, result)
}
