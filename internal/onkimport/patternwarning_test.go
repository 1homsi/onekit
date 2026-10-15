package onkimport

import (
	"strings"
	"testing"
)

func TestImportWarnsAboutAStringPatternItCannotCarry(t *testing.T) {
	spec := `
openapi: 3.0.3
info: { title: Patterns, version: 1.0.0 }
paths:
  /codes:
    get:
      operationId: getCode
      responses:
        "200":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Code" }
components:
  schemas:
    Code:
      type: object
      properties:
        slug: { type: string, pattern: "^[a-z]+$" }
`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range result.Warnings {
		found = found || strings.Contains(warning, `"^[a-z]+$" is not imported`)
	}
	if !found {
		t.Fatalf("a dropped pattern must be reported, warnings: %v", result.Warnings)
	}
}
