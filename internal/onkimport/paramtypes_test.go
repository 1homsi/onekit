package onkimport

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestImportBindsEnumAndTimestampParameters(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets/{status}":{"get":{"operationId":"findPets",
"parameters":[{"name":"status","in":"path","required":true,"schema":{"type":"string","enum":["available","sold"]}},
{"name":"since","in":"query","schema":{"type":"string","format":"date-time"}}],
"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, `status: string @in("available", "sold")`) || !strings.Contains(src, "since: string? @query") {
		t.Fatalf("parameters not bindable:\n%s", src)
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "pets.onk", AST: ast}}); err != nil {
		t.Fatalf("import does not compile: %v\n%s", err, src)
	}
}
