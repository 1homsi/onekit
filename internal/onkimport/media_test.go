package onkimport

import (
	"strings"
	"testing"
)

func TestImportAcceptsJSONMediaTypeVariants(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{"/pets":{"post":{"operationId":"createPet",
"requestBody":{"content":{"application/json; charset=utf-8":{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}}},
"responses":{"200":{"description":"ok","content":{"application/json;charset=UTF-8":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}},
"422":{"description":"bad","content":{"application/problem+json":{"schema":{"type":"object","properties":{"detail":{"type":"string"}}}}}}}}},
"/upload":{"post":{"operationId":"upload","requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object"}}}},"responses":{"204":{"description":"none"}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"name: string", "id: string", "detail: string", "@status(422)"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	found := false
	for _, warning := range result.Warnings {
		found = found || strings.Contains(warning, "multipart/form-data")
	}
	if !found {
		t.Fatalf("no warning for skipped multipart body: %v", result.Warnings)
	}
}
