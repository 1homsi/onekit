package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIDocumentsGeneratedErrorResponses(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model GetUser {
  id: string;
}

model User {
  id: string;
}

@error
model Invalid {
  @statusCode _: 400;
  field: string;
}

/** Headers every Users operation takes. */
alias UsersHeaders = {
  @header("Authorization") @Onkt.auth("bearer") header_authorization: string;
};

interface Users {
  @get
  @route("/users/{id}")
  get(...UsersHeaders, ...GetUser): User;

  @post
  @route("/users/check")
  check(...UsersHeaders, ...GetUser): User | Invalid;
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
