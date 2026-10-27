package genrust

import (
	"strings"
	"testing"
)

func TestRustServerBindsRepeatedQueryParametersByPair(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `namespace app;

model Filter {
  id: string;
  @query
  tags: string[];
  @query
  limit?: int32;
}

model Item {
  id: string;
}

interface S {
  @get
  @route("/items/{id}")
  list(...Filter): Item;
}
`)))
	for _, want := range []string{
		"query: Result<Query<Vec<(String, String)>>, axum::extract::rejection::QueryRejection>,",
		"let mut req = Filter::default();",
		`"tags" => match parse_path(value) {`,
		"Ok(value) => req.tags.push(value),",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("repeated @query parameters must be bound pair by pair, missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Query<Filter>") {
		t.Fatalf("axum's Query<T> cannot read repeated keys into a Vec:\n%s", out)
	}
}

func TestRustServerKeepsTypedQueryExtractionWithoutRepeatedParameters(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, `namespace app;

model Filter {
  id: string;
  @query
  limit?: int32;
}

model Item {
  id: string;
}

interface S {
  @get
  @route("/items/{id}")
  list(...Filter): Item;
}
`)))
	if !strings.Contains(out, "query: Result<Query<Filter>, axum::extract::rejection::QueryRejection>,") {
		t.Fatalf("scalar-only query requests keep the typed extractor:\n%s", out)
	}
}
