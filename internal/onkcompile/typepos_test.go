package onkcompile

import (
	"errors"
	"testing"
)

func TestCompileReportsUnresolvedTypesAtTheType(t *testing.T) {
	tests := []struct {
		schema    string
		line, col int
	}{
		{"message R {}\nservice S {\n  a(Missing) -> R @get(\"/a\")\n}\n", 3, 5},
		{"message R {}\nservice S {\n  a(R) -> Nope @get(\"/a\")\n}\n", 3, 11},
		{"message R {}\nservice S {\n  a(R) -> R | Gone @get(\"/a\")\n}\n", 3, 15},
		{"message M {\n  values: map[string, Missing]\n}\n", 2, 23},
		{"message M {\n  value: Missing\n}\n", 2, 10},
	}
	for _, tt := range tests {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, tt.schema)}})
		var compileErr *Error
		if !errors.As(err, &compileErr) || compileErr.Line != tt.line || compileErr.Column != tt.col {
			t.Fatalf("%q: want %d:%d, got %v", tt.schema, tt.line, tt.col, err)
		}
	}
}
