package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestGenerateListsRequiredScopesUnderTheAuthScheme(t *testing.T) {
	src := `
package app
message Item { id: string }
message GetItem { id: string }
service API {
  headers: { "Authorization": string @required @auth("bearer") @auth_scheme_name("UserAuth") }
  get(GetItem) -> Item @get("/items/{id}") @requires("items:read", "items:write")
  ping(GetItem) -> Item @get("/ping/{id}")
}
`
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := Generate(pkg.Files[0], Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	spec := string(out)
	for _, want := range []string{"- UserAuth:", "- items:read", "- items:write", "- UserAuth: []"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("missing %q in:\n%s", want, spec)
		}
	}
	if strings.Count(spec, "- UserAuth: []") != 1 {
		t.Fatalf("the route without @requires keeps an empty scope list:\n%s", spec)
	}
}
