package genopenapi

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestNullableFieldsAllowNullInSchema(t *testing.T) {
	ast, err := onklang.Parse(`package app

message Item { name: string }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable
  item: Item? @nullable
  plain: string?
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
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
