package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIOneofMatchesWireShape(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message EmailAuth { email: string }
message Login {
  method: oneof(discriminator: "auth_type") {
    email: EmailAuth @tag("email")
    token: string @tag("token")
  }
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"type: object properties: auth_type: type: string const: email email: $ref: '#/components/schemas/app.EmailAuth' title: email required: - auth_type - email",
		"type: object properties: auth_type: type: string const: token token: type: string title: token required: - auth_type - token",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "discriminator:") {
		t.Fatalf("discriminator mapping cannot point at inline variants:\n%s", doc)
	}
}
