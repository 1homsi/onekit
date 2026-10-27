package onkcompile

import (
	"strings"
	"testing"
)

func TestDeclarationsMustStartUppercase(t *testing.T) {
	for _, src := range []string{
		`
model user {
  id: string;
}
`,
		`
enum state {
  ON,
}
`,
		`
model R {}

interface users {
  @get
  @route("/u")
  get(...R): R;
}
`,
	} {
		_, err := Compile([]Source{{Path: "a.tsp", AST: parseOrFatal(t, src)}})
		if err == nil || !strings.Contains(err.Error(), "must start with an uppercase letter") {
			t.Fatalf("%s: err = %v", src, err)
		}
	}
}
