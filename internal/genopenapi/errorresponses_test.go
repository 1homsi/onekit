package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIDocumentsGeneratedErrorResponses(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message GetUser { id: string }
message User { id: string }
message Invalid @status(400) { field: string }
service Users {
  headers: { "Authorization": string @required @auth("bearer") }
  get(GetUser) -> User @get("/users/{id}")
  check(GetUser) -> User | Invalid @post("/users/check")
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		`"400": description: Invalid request content: application/json: schema: $ref: '#/components/schemas/onekit.ErrorMessage'`,
		`"401": description: Unauthorized`,
		`default: description: Unexpected error`,
		`"400": description: Invalid content: application/json: schema: $ref: '#/components/schemas/app.Invalid'`,
		`onekit.ErrorMessage: type: object properties: message: type: string required: - message`,
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
