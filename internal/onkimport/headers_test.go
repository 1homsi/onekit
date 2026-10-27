package onkimport

import (
	"strings"
	"testing"
)

func TestImportConvertsHeaderParametersAndSecurity(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},
"security":[{"ApiKey":[]}],
"components":{"securitySchemes":{
  "ApiKey":{"type":"apiKey","in":"header","name":"X-API-Key"},
  "Jwt":{"type":"http","scheme":"bearer"}
}},
"paths":{
  "/pets":{
    "get":{"operationId":"listPets","parameters":[
      {"name":"X-Request-Id","in":"header","required":true,"schema":{"type":"string","format":"uuid"}},
      {"name":"Accept","in":"header","schema":{"type":"string"}}
    ],"responses":{"200":{"description":"ok"}}},
    "post":{"operationId":"createPet","security":[{"Jwt":[]}],"responses":{"200":{"description":"ok"}}},
    "delete":{"operationId":"clearPets","security":[],"responses":{"200":{"description":"ok"}}}
  }
}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	requireFaithfulTypeSpec(t, result)
	for _, want := range []string{
		`@header("X-API-Key") @Onkt.auth("api_key", "ApiKey") header_x_api_key: string`,
		`@header("X-Request-Id") @format("uuid") header_x_request_id: string`,
		`@header("Authorization") @Onkt.auth("bearer", "Jwt") header_authorization: string`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, `"Accept"`) || strings.Count(src, "X-API-Key") != 1 {
		t.Fatalf("unexpected headers:\n%s", src)
	}
}
