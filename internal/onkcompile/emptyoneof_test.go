package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsEmptyOneof(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
union MP {
}

model M {
  p: MP;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), `oneof "p" must declare at least one variant`) {
		t.Fatalf("want empty oneof error, got %v", err)
	}
}
