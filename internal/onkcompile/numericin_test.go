package onkcompile

import (
	"strings"
	"testing"
)

func TestInAcceptsIntegersWithinTheFieldRange(t *testing.T) {
	for src, want := range map[string]string{
		"message M { size: int32 @in(1, 2) }": "",
		"message M { size: uint32 @in(-1) }":  `@in value "-1" must be an integer within the range of uint32`,
		"message M { ratio: float64 @in(1) }": "@in requires a non-repeated string or integer field",
		"message M { sizes: int32[] @in(1) }": "@in requires a non-repeated string or integer field",
		"message M { size: int32 @in(1.5) }":  `@in value "1.5" must be an integer`,
	} {
		_, err := Compile([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}})
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatalf("%s: err = %v, want %q", src, err, want)
		}
	}
}
