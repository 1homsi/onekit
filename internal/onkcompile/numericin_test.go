package onkcompile

import (
	"strings"
	"testing"
)

func TestInAcceptsIntegersWithinTheFieldRange(t *testing.T) {
	for src, want := range map[string]string{
		`
model M {
  @Onkt.values(1, 2)
  size: int32;
}
`: "",
		`
model M {
  @Onkt.values(-1)
  size: uint32;
}
`: `@in value "-1" must be an integer within the range of uint32`,
		`
model M {
  @Onkt.values(1)
  ratio: float64;
}
`: "@in requires a non-repeated string or integer field",
		`
model M {
  @Onkt.values(1)
  sizes: int32[];
}
`: "@in requires a non-repeated string or integer field",
		`
model M {
  @Onkt.values(1.5)
  size: int32;
}
`: `@in value "1.5" must be an integer`,
	} {
		_, err := Compile([]Source{{Path: "a.tsp", AST: parseOrFatal(t, src)}})
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatalf("%s: err = %v, want %q", src, err, want)
		}
	}
}
