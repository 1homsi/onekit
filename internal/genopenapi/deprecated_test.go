package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIMarksDeprecatedFieldsAndOperations(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message Note { title: string @deprecated }
service Notes { get(Note) -> Note @get("/notes") @deprecated("use fetch") }
`)
	flat := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{"$ref: '#/components/schemas/onekit.ErrorMessage' deprecated: true", "title: type: string deprecated: true"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in:\n%s", want, doc)
		}
	}
}
