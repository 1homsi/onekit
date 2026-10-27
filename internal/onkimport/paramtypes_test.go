package onkimport

import (
	"strings"
	"testing"
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
	if !strings.Contains(src, `  status: "available" | "sold";`) || !strings.Contains(src, "  @query\n  since?: string;\n") {
		t.Fatalf("parameters not bindable:\n%s", src)
	}
	requireFaithfulTypeSpec(t, result)
}
