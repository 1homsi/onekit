package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportDeclaresUndeclaredPathParameters(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/owners/{owner-id}/pets/{petId}":{"get":{"operationId":"getPet",
"parameters":[{"name":"petId","in":"path","required":true,"schema":{"type":"integer"}}],"responses":{"200":{"description":"ok"}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
	if !strings.Contains(src, "owner_id: string") || !strings.Contains(src, `@get("/owners/{owner_id}/pets/{petId}")`) {
		t.Fatalf("undeclared parameter not imported:\n%s", src)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), `path parameter "owner-id" is not declared`) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}
