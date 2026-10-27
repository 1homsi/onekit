package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIOneofMatchesWireShape(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model EmailAuth {
  email: string;
}

@Onkt.discriminator("auth_type")
union LoginMethod {
  @Onkt.tag("email") email: EmailAuth,
  @Onkt.tag("token") token: string,
}

model Login {
  method: LoginMethod;
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

func TestOpenAPIFlattenedOneofMergesVariantFields(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model EmailAuth {
  email: string;
}

@discriminated(#{ discriminatorPropertyName: "auth_type", envelope: "none" })
union LoginMethod {
  @Onkt.tag("email") email: EmailAuth,
}

model Login {
  method: LoginMethod;
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	want := "- allOf: - type: object properties: auth_type: type: string const: email required: - auth_type - $ref: '#/components/schemas/app.EmailAuth' title: email"
	if !strings.Contains(flat, want) {
		t.Fatalf("missing %q in:\n%s", want, doc)
	}
}
