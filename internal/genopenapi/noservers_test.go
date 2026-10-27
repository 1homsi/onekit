package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIOmitsServersWhenNoneAreConfigured(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model R {}

interface S {
  @get
  @route("/r")
  get(...R): R;
}
`)
	if strings.Contains(doc, "servers:") {
		t.Fatalf("empty servers list emitted:\n%s", doc)
	}
}
