package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIEnumTypesMatchTheField(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model Page {
  @Onkt.values(10, 25)
  size: int32;
  code: "1" | "2";
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"enum: - 10 - 25", `enum: - "1" - "2"`} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
