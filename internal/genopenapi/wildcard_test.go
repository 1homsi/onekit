package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const wildcardSchema = `namespace app;

model FileRef {
  path: string;
}

model Content {
  path: string;
}

@route("/files")
interface Files {
  @get
  @route("/{+path}")
  read(...FileRef): Content;
}
`

func compileWildcard(t *testing.T) []byte {
	t.Helper()
	ast, err := onktest.Parse(wildcardSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	_ = file
	return func() []byte {
		out, err := Generate(file, Options{})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}()
}

func TestWildcardPathParameter(t *testing.T) {
	out := string(compileWildcard(t))
	for _, want := range []string{`/files/{path}:`, `x-onekit-wildcard: true`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
