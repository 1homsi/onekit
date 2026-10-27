package genpy

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestPythonClientWarnsOnDeprecatedMethods(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model Note {
  id: string;
}

interface Notes {
  #deprecated "use fetch"
  @get
  @route("/notes/{id}")
  get(...Note): Note;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	client := string(GenerateClient(pkg.Files[0], "models"))
	if !strings.Contains(client, `warnings.warn("get is deprecated: use fetch", DeprecationWarning, stacklevel=2)`) {
		t.Fatalf("missing deprecation warning:\n%s", client)
	}
}
