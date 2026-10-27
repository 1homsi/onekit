package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestNullableFieldsAllowNullInSchema(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model Item {
  name: string;
}

model Patch {
  id: int32;
  folder_id?: int64 | null;
  note?: string | null;
  item?: Item | null;
  plain?: string;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(pkg.Files[0], Options{})
	if err != nil {
		t.Fatal(err)
	}
	spec := string(out)
	nullableMentions := strings.Count(spec, "null")
	if nullableMentions < 3 {
		t.Errorf("expected null in the schema of the three nullable fields, got %d mentions:\n%s", nullableMentions, spec)
	}
	at := strings.Index(spec, "plain:")
	if at < 0 {
		t.Fatalf("plain field missing from the spec:\n%s", spec)
	}
	plain := spec[at:]
	if strings.Contains(strings.SplitN(plain, "\n\n", 2)[0], "null") {
		t.Errorf("a plain optional field must not allow null:\n%s", plain)
	}
}
