package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIBodyLeavesOutPathParameters(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message UpdateUser { id: string name: string }
message Touch { id: string }
message User { id: string }
service Users {
  update(UpdateUser) -> User @put("/users/{id}")
  touch(Touch) -> User @post("/users/{id}/touch")
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"requestBody: content: application/json: schema: type: object properties: name: type: string required: - name required: true",
		"requestBody: content: application/json: schema: type: object required: false responses",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
