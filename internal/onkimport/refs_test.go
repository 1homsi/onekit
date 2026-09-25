package onkimport

import (
	"strings"
	"testing"
)

func TestImportResolvesReferencedParametersBodiesAndResponses(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},
"components":{
  "parameters":{"Limit":{"name":"limit","in":"query","schema":{"type":"integer"}}},
  "requestBodies":{"NewPet":{"required":true,"content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}}},
  "responses":{
    "Pet":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}},
    "NotFound":{"description":"missing","content":{"application/json":{"schema":{"type":"object","properties":{"reason":{"type":"string"}}}}}}
  }
},
"paths":{
  "/pets":{
    "get":{"operationId":"listPets","parameters":[{"$ref":"#/components/parameters/Limit"}],"responses":{"200":{"$ref":"#/components/responses/Pet"}}},
    "post":{"operationId":"createPet","requestBody":{"$ref":"#/components/requestBodies/NewPet"},"responses":{"200":{"$ref":"#/components/responses/Pet"},"404":{"$ref":"#/components/responses/NotFound"}}}
  }
}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"limit: int32? @query", "name: string", "id: string", "reason: string", "@status(404)", `@body("body")`} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
}
