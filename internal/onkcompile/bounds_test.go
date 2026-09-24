package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsBoundsTheFieldTypeCannotHold(t *testing.T) {
	tests := map[string]string{
		`s: string @len(1.5, 3)`:          "non-negative integer",
		`s: string[] @min_items(1.5)`:     "non-negative integer",
		`n: int32 @gte(1.5)`:              "within the range of int32",
		`n: uint32 @range(-5, 10)`:        "within the range of uint32",
		`n: int32 @range(0, 99999999999)`: "within the range of int32",
		`n: float64 @lte("NaN")`:          "finite decimal",
		`n: float64 @gt("0x1p4")`:         "finite decimal",
	}
	for field, want := range tests {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, "message M { "+field+" }")}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want %q, got %v", field, want, err)
		}
	}
	for _, field := range []string{`n: float64 @range(0.5, 1e6)`, `n: int64 @gte(-9223372036854775808)`} {
		if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, "message M { "+field+" }")}}); err != nil {
			t.Fatalf("%s: rejected valid bound: %v", field, err)
		}
	}
}
