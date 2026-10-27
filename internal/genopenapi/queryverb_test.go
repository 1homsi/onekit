package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIDeclaresVersionThatSupportsQuery(t *testing.T) {
	schema := `
namespace app;
model Search { term: string; }
model Results { ids: string[]; }
interface Items { %s }
`
	withQuery := openAPIForSchema(t, strings.Replace(schema, "%s", `@Onkt.method("QUERY") @route("/items/search") search(...Search): Results;`, 1))
	if !strings.HasPrefix(withQuery, "openapi: 3.2.0\n") || !strings.Contains(withQuery, "query:") {
		t.Fatalf("QUERY route needs OpenAPI 3.2:\n%s", withQuery)
	}
	withoutQuery := openAPIForSchema(t, strings.Replace(schema, "%s", `@post @route("/items/search") search(...Search): Results;`, 1))
	if !strings.HasPrefix(withoutQuery, "openapi: 3.1.0\n") {
		t.Fatalf("plain routes keep OpenAPI 3.1:\n%s", withoutQuery)
	}
}
