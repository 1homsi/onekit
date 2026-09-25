package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIDocumentsSSEErrorEvents(t *testing.T) {
	doc := openAPIForSchema(t, sseFixtureSrc)
	flat := strings.Join(strings.Fields(doc), " ")
	want := "x-sse-error-schemas: - $ref: '#/components/schemas/app.StreamError' - $ref: '#/components/schemas/onekit.ErrorMessage'"
	if !strings.Contains(flat, want) {
		t.Fatalf("missing %q in:\n%s", want, doc)
	}
}
