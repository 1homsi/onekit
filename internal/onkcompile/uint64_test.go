package onkcompile

import "testing"

func TestCompileAcceptsMaxUint64Bound(t *testing.T) {
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message M { n: uint64 @lte(18446744073709551615) }
`)}}); err != nil {
		t.Fatalf("max uint64 bound rejected: %v", err)
	}
}
