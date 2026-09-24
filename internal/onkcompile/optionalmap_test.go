package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsOptionalMaps(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message Item { name: string }
message M { items: map[string, Item]? }
`)}})
	if err == nil || !strings.Contains(err.Error(), `map field "items" cannot be optional`) {
		t.Fatalf("want optional map error, got %v", err)
	}
}
