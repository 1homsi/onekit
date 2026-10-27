package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIPathAndQueryParametersKeepFieldContracts(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model GetUser {
  /** The user's identifier. */
  @format("uuid")
  id: string;
  /** Number of results. */
  @query
  @Onkt.range(1, 100)
  limit?: int32;
}

model User {
  id: string;
}

interface Users {
  @get
  @route("/users/{id}")
  get(...GetUser): User;
}
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
