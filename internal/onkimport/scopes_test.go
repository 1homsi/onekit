package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportTurnsOperationScopesIntoRequires(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},
"components":{"securitySchemes":{"Jwt":{"type":"http","scheme":"bearer"}}},
"paths":{
  "/pets":{
    "get":{"operationId":"listPets","security":[{"Jwt":["pets:read"]}],"responses":{"200":{"description":"ok"}}},
    "post":{"operationId":"createPet","security":[{"Jwt":["pets:read","pets:write","bad scope","pets:read"]}],"responses":{"200":{"description":"ok"}}},
    "delete":{"operationId":"clearPets","security":[{"Jwt":[]}],"responses":{"200":{"description":"ok"}}}
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
	for _, want := range []string{`@requires("pets:read")`, `@requires("pets:read", "pets:write")`} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	if strings.Count(src, "@requires") != 2 {
		t.Fatalf("an empty scope list must not add @requires:\n%s", src)
	}
	warned := false
	for _, w := range result.Warnings {
		if strings.Contains(w, `security scope "bad scope"`) {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("a scope @requires cannot express must be reported: %v", result.Warnings)
	}
}
