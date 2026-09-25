package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIMarksBareDeprecatedHeaders(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message R {}
service S {
  headers: {
    "X-Old": string @deprecated
    "X-Older": string @deprecated("use X-New")
  }
  get(R) -> R @get("/r")
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"name: X-Old in: header required: false deprecated: true", "name: X-Older in: header required: false deprecated: true"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
