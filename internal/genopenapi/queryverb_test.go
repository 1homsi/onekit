package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIDeclaresVersionThatSupportsQuery(t *testing.T) {
	schema := `
package app
message Search { term: string }
message Results { ids: string[] }
service Items { %s }
`
	withQuery := openAPIForSchema(t, strings.Replace(schema, "%s", `search(Search) -> Results @query("/items/search")`, 1))
	if !strings.HasPrefix(withQuery, "openapi: 3.2.0\n") || !strings.Contains(withQuery, "query:") {
		t.Fatalf("QUERY route needs OpenAPI 3.2:\n%s", withQuery)
	}
	withoutQuery := openAPIForSchema(t, strings.Replace(schema, "%s", `search(Search) -> Results @post("/items/search")`, 1))
	if !strings.HasPrefix(withoutQuery, "openapi: 3.1.0\n") {
		t.Fatalf("plain routes keep OpenAPI 3.1:\n%s", withoutQuery)
	}
}
