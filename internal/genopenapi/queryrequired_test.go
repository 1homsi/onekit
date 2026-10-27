package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIQueryParametersRequiredOnlyWhenDeclared(t *testing.T) {
	doc := openAPIForSchema(t, `namespace app;

model List {
  @query
  page: int32;
  @query
  @Onkt.required
  cursor: string;
}

interface S {
  @get
  @route("/items")
  list(...List): List;
}
`)
	flat := strings.Join(strings.Fields(doc), " ")
	if strings.Contains(flat, "name: page in: query required: true") || !strings.Contains(flat, "name: cursor in: query required: true") {
		t.Fatalf("query required flags wrong:\n%s", doc)
	}
}
