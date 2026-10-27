package genopenapi

import (
	"slices"
	"testing"

	"github.com/pb33f/go-yaml"
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
	doc := openAPIForSchema(t, `namespace app;

model Meta {
  owner: string;
}

model Audit {
  by: string;
}

model Item {
  id: string;
  @Onkt.flatten("meta_")
  meta?: Meta;
  @Onkt.flatten("audit_")
  audit: Audit;
  @Onkt.empty("omit")
  extra: Meta;
  @Onkt.empty("null")
  kept: Meta;
}
`)
	got := requiredOf(t, doc, "app.Item")
	if !slices.Equal(got, []string{"id", "audit_by", "kept"}) {
		t.Fatalf("required = %v\n%s", got, doc)
	}
}
