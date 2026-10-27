package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIBodyLeavesOutPathParameters(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model UpdateUser {
  id: string;
  name: string;
}

model Touch {
  id: string;
}

model User {
  id: string;
}

interface Users {
  @put
  @route("/users/{id}")
  update(...UpdateUser): User;

  @post
  @route("/users/{id}/touch")
  touch(...Touch): User;
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
