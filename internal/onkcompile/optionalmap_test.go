package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsOptionalMaps(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model Item {
  name: string;
}

model M {
  items?: Record<Item>;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), `map field "items" cannot be optional`) {
		t.Fatalf("want optional map error, got %v", err)
	}
}
