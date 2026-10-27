package onkimport

import (
	"strings"
	"testing"
)

func TestImportKeepsAnOptionalMapAsAPlainMap(t *testing.T) {
	spec := `
openapi: 3.0.3
info: { title: Maps, version: 1.0.0 }
paths:
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Thing" }
components:
  schemas:
    Thing:
      type: object
      required: [id]
      properties:
        id: { type: string }
        labels:
          type: object
          additionalProperties: { type: string }
        tags:
          type: array
          items: { type: string }
`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatalf("an optional additionalProperties object must import: %v", err)
	}
	source := string(result.Source)
	if !strings.Contains(source, "  labels: Record<string>;\n") || strings.Contains(source, "labels?:") {
		t.Fatalf("the map must stay a plain map:\n%s", source)
	}
	requireFaithfulTypeSpec(t, result)
}
