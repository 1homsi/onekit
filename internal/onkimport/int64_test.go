package onkimport

import (
	"strings"
	"testing"
)

func TestImportKeepsInt64AsJSONNumbers(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Ledger","version":"1"},"paths":{"/entries":{"get":{"operationId":"listEntries","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"amount":{"type":"integer","format":"int64"},"ids":{"type":"array","items":{"type":"integer","format":"int64"}}}}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	if !strings.Contains(src, "amount: int64? @encode(number)") && !strings.Contains(src, "amount: int64 @encode(number)") {
		t.Fatalf("int64 not number-encoded:\n%s", src)
	}
	found := false
	for _, warning := range result.Warnings {
		found = found || strings.Contains(warning, "int64 array items")
	}
	if !found {
		t.Fatalf("missing int64 array warning: %v", result.Warnings)
	}
}
