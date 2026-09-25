package onkimport

import (
	"strings"
	"testing"
)

func TestImportAcceptsUnquotedYAMLStatusCodes(t *testing.T) {
	spec := `openapi: 3.0
info: { title: Pets, version: "1" }
paths:
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        200:
          description: ok
          content:
            application/json:
              schema: { type: object, properties: { name: { type: string } } }
        404:
          description: missing
          content:
            application/json:
              schema: { type: object, properties: { reason: { type: string } } }
`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"name: string", "reason: string", "@status(404)"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s\nwarnings: %v", want, src, result.Warnings)
		}
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "no 2xx response") {
			t.Fatalf("200 response lost: %v", result.Warnings)
		}
	}
}
