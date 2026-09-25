package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIEnumTypesMatchTheField(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message Page {
  size: int32 @in(10, 25)
  code: string @in("1", "2")
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"enum: - 10 - 25", `enum: - "1" - "2"`} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
