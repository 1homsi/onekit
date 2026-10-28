package genopenapi

import (
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

// Every item of a list or map of plainDate is a YYYY-MM-DD string, as a
// single plainDate is.
func TestListsAndMapsOfDatesDescribeDateItems(t *testing.T) {
	ast, err := onktest.Parse(`namespace app;

model Trip {
  day: plainDate;
  days: plainDate[];
  byCity: Record<plainDate>;
  stamps: utcDateTime[];
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(pkg.Files[0], Options{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Format               string                   `json:"format"`
					Items                *struct{ Format string } `json:"items"`
					AdditionalProperties *struct{ Format string } `json:"additionalProperties"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	props := doc.Components.Schemas["app.Trip"].Properties
	if got := props["day"].Format; got != "date" {
		t.Errorf("day format = %q, want date", got)
	}
	if items := props["days"].Items; items == nil || items.Format != "date" {
		t.Errorf("days items = %+v, want format date", items)
	}
	if values := props["byCity"].AdditionalProperties; values == nil || values.Format != "date" {
		t.Errorf("byCity values = %+v, want format date", values)
	}
	if items := props["stamps"].Items; items == nil || items.Format != "date-time" {
		t.Errorf("stamps items = %+v, want format date-time", items)
	}
}
