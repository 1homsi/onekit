package onkimport

import (
	"strings"
	"testing"
)

func TestImportKeepsRecursiveComponentReferences(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Tree","version":"1"},"components":{"schemas":{"Node":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/components/schemas/Node"}},"parent":{"$ref":"#/components/schemas/Node"}}}}},
"paths":{"/tree":{"get":{"operationId":"getTree","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Node"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, "  children: Node[];\n") || !strings.Contains(src, "  parent?: Node;\n") {
		t.Fatalf("recursive references lost:\n%s\n%v", src, result.Warnings)
	}
	requireFaithfulTypeSpec(t, result)
}
