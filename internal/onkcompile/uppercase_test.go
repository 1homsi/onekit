package onkcompile

import (
	"strings"
	"testing"
)

func TestDeclarationsMustStartUppercase(t *testing.T) {
	for _, src := range []string{
		"message user { id: string }",
		"enum state { ON }",
		"message R {}\nservice users { get(R) -> R @get(\"/u\") }",
	} {
		_, err := Compile([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}})
		if err == nil || !strings.Contains(err.Error(), "must start with an uppercase letter") {
			t.Fatalf("%s: err = %v", src, err)
		}
	}
}
