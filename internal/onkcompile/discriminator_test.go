package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsVariantNamedLikeDiscriminator(t *testing.T) {
	for _, schema := range []string{
		`
model A {}

union MP {
  type: A,
}

model M {
  p: MP;
}
`,
		`
model A {}

@Onkt.discriminator("kind")
union MP {
  kind: A,
}

model M {
  p: MP;
}
`,
	} {
		_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), "same JSON key as the discriminator") {
			t.Fatalf("%s: want discriminator collision error, got %v", schema, err)
		}
	}
}
