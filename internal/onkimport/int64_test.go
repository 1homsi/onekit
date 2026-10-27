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
	// TypeSpec int64 values and lists are JSON numbers; only an explicit
	// string encoding would change that.
	for _, want := range []string{"  amount?: int64;\n", "  ids: int64[];\n"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "@encode(string)") || strings.Contains(src, `@Onkt.encode("string")`) {
		t.Fatalf("int64 imported as a string:\n%s", src)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "int64") {
			t.Fatalf("a faithful int64 import needs no warning: %v", result.Warnings)
		}
	}
	requireFaithfulTypeSpec(t, result)
}
