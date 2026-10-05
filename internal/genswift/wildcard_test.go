package genswift

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const wildcardSchema = `package app

message FileRef { path: string }
message Content { path: string }

service Files {
  base_path: "/files"
  read(FileRef) -> Content @get("/{path...}")
}
`

func compileWildcard(t *testing.T) []byte {
	t.Helper()
	ast, err := onklang.Parse(wildcardSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	_ = file
	return GenerateClient(file)
}

func TestWildcardPathParameter(t *testing.T) {
	out := string(compileWildcard(t))
	for _, want := range []string{"dot segments are not allowed", `"{path...}"`, `onekitPathWildcard(req.path)`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
