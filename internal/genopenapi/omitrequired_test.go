package genopenapi

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v4"
)

func requiredOf(t *testing.T, doc, name string) []string {
	t.Helper()
	var parsed struct {
		Components struct {
			Schemas map[string]struct {
				Required []string `yaml:"required"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Components.Schemas[name].Required
}

func TestOpenAPIDoesNotRequireFieldsTheServerOmits(t *testing.T) {
	doc := openAPIForSchema(t, `
package app
message Meta { owner: string }
message Audit { by: string }
message Item {
  id: string
  meta: Meta? @flatten(prefix: "meta_")
  audit: Audit @flatten(prefix: "audit_")
  extra: Meta @empty("omit")
  kept: Meta @empty("null")
}
`)
	got := requiredOf(t, doc, "app.Item")
	if !slices.Equal(got, []string{"id", "audit_by", "kept"}) {
		t.Fatalf("required = %v\n%s", got, doc)
	}
}
