package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportKeepsRecursiveComponentReferences(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Tree","version":"1"},"components":{"schemas":{"Node":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/components/schemas/Node"}},"parent":{"$ref":"#/components/schemas/Node"}}}}},
"paths":{"/tree":{"get":{"operationId":"getTree","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Node"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, "children: Node[]") || !strings.Contains(src, "parent: Node?") {
		t.Fatalf("recursive references lost:\n%s\n%v", src, result.Warnings)
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "tree.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
}
