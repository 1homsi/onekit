package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIPathAndQueryParametersKeepFieldContracts(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message GetUser {
  /// The user's identifier.
  id: string @uuid
  /// Number of results.
  limit: int32? @query @range(1, 100)
}
message User { id: string }
service Users { get(GetUser) -> User @get("/users/{id}") }
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"name: id in: path description: The user's identifier. required: true schema: type: string format: uuid",
		"name: limit in: query description: Number of results.",
		"maximum: 100 minimum: 1",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
