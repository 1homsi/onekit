package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIQueryParametersRequiredOnlyWhenDeclared(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message List {
  page: int32 @query
  cursor: string @query @required
}
service S { list(List) -> List @get("/items") }
`)
	flat := strings.Join(strings.Fields(doc), " ")
	if strings.Contains(flat, "name: page in: query required: true") || !strings.Contains(flat, "name: cursor in: query required: true") {
		t.Fatalf("query required flags wrong:\n%s", doc)
	}
}
