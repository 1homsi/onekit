package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsBasePathParameters(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R { tenant: string }
service API {
  base_path: "/t/{tenant}"
  a(R) -> R @get("/x")
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "base_path must not contain path parameters") {
		t.Fatalf("want base_path parameter error, got %v", err)
	}
}
