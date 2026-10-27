package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsBasePathParameters(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model R {
  tenant: string;
}

@route("/t/{tenant}")
interface API {
  @get
  @route("/x")
  a(...R): R;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "base_path must not contain path parameters") {
		t.Fatalf("want base_path parameter error, got %v", err)
	}
}
