package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
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
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
	for _, want := range []string{
		`"X-API-Key": string @required @auth("api_key") @auth_scheme_name("ApiKey")`,
		`"X-Request-Id": string @required @format("uuid")`,
		`"Authorization": string @required @auth("bearer") @auth_scheme_name("Jwt")`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, `"Accept"`) || strings.Count(src, "X-API-Key") != 1 {
		t.Fatalf("unexpected headers:\n%s", src)
	}
}
