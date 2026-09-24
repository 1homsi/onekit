package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsEmptyEnum(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
enum E {}
message M { e: E }
`)}})
	if err == nil || !strings.Contains(err.Error(), `enum "E" must declare at least one value`) {
		t.Fatalf("want empty enum error, got %v", err)
	}
}
