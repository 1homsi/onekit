package onkcompile

import "testing"

func TestCompileAcceptsMaxUint64Bound(t *testing.T) {
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model M {
  @maxValue(18446744073709551615)
  @encode(string)
  n: uint64;
}
`)}}); err != nil {
		t.Fatalf("max uint64 bound rejected: %v", err)
	}
}
