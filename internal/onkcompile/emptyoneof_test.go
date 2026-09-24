package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsEmptyOneof(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message M { p: oneof {} }
`)}})
	if err == nil || !strings.Contains(err.Error(), `oneof "p" must declare at least one variant`) {
		t.Fatalf("want empty oneof error, got %v", err)
	}
}
