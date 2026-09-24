package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsVariantNamedLikeDiscriminator(t *testing.T) {
	for _, schema := range []string{
		`message A {} message M { p: oneof { type: A } }`,
		`message A {} message M { p: oneof(discriminator: "kind") { kind: A } }`,
	} {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), "same JSON key as the discriminator") {
			t.Fatalf("%s: want discriminator collision error, got %v", schema, err)
		}
	}
}
