package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestGenerateListsRequiredScopesUnderTheAuthScheme(t *testing.T) {
	src := `namespace app;

model Item {
  id: string;
}

model GetItem {
  id: string;
}

/** Headers every API operation takes. */
alias APIHeaders = {
  @header("Authorization") @Onkt.auth("bearer", "UserAuth") header_authorization: string;
};

interface API {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read", "items:write")
  get(...APIHeaders, ...GetItem): Item;

  @get
  @route("/ping/{id}")
  ping(...APIHeaders, ...GetItem): Item;
}
`
	ast, err := onktest.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.tsp", AST: ast}})
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
