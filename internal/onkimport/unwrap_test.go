package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportUnwrapsNonObjectResponses(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{
"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"array","items":{"type":"string"}}}}}}}},
"/count":{"get":{"operationId":"countPets","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"integer"}}}}}}},
"/tags":{"get":{"operationId":"tagCounts","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","additionalProperties":{"type":"integer"}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if strings.Count(src, "@unwrap") != 3 {
		t.Fatalf("responses not unwrapped:\n%s", src)
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("unwrapped import does not compile: %v\n%s", err, src)
	}
}
