package onktest

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

const header = "import \"@typespec/http\";\nimport \"@onkt/typespec\";\nusing TypeSpec.Http;\n"

func TestParseReturnsDeclarations(t *testing.T) {
	file := MustParse(t, header+"namespace app;\nmodel User { id: string; }\n")
	if file.Package != "app" || len(file.Messages) != 1 || file.Messages[0].Name != "User" {
		t.Fatalf("file = %+v", file)
	}
}

func TestParseReportsTypeSpecDiagnostics(t *testing.T) {
	_, err := Parse(header + "model User { id: Missing; }\n")
	var diags *DiagnosticsError
	if !errors.As(err, &diags) || !strings.Contains(diags.Lines[0], "schema.tsp:4:18 - error invalid-ref") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileWithoutMainImportsEveryFile(t *testing.T) {
	doc, err := Compile(map[string]string{
		"a/models.tsp": header + "namespace a;\nmodel User { id: string; }\n",
		"b/api.tsp":    header + "namespace b;\nmodel Get { user: a.User; }\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Files) != 2 || doc.Files[0].Path != "a/models.tsp" || doc.Files[1].Path != "b/api.tsp" {
		t.Fatalf("files = %+v", doc.Files)
	}
}

func TestCompileRunsConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Parse(header + "namespace app;\nmodel M { n: int32; }\n"); err != nil {
				t.Errorf("compile %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
}
