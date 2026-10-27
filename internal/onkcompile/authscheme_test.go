package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsConflictingAuthSchemes(t *testing.T) {
	for _, schema := range []string{`
model R {}

interface API {
  @get
  @route("/a")
  a(
    @header("X-Key") @Onkt.auth("api_key") header_x_key: string,
    ...R,
  ): R;

  @get
  @route("/b")
  b(
    @header("Authorization") @Onkt.auth("bearer", "X-KeyAuth") header_authorization: string,
    ...R,
  ): R;
}
`, `
model R {}

interface API {
  @get
  @route("/a")
  a(
    @header("X-One") @Onkt.auth("api_key", "Main") header_x_one: string,
    ...R,
  ): R;

  @get
  @route("/b")
  b(
    @header("X-Two") @Onkt.auth("api_key", "Main") header_x_two: string,
    ...R,
  ): R;
}
`} {
		_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), "is declared as both") {
			t.Fatalf("want conflicting scheme error, got %v", err)
		}
	}
}
