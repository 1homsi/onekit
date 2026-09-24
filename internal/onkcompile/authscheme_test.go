package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsConflictingAuthSchemes(t *testing.T) {
	for _, schema := range []string{`
message R {}
service API {
  a(R) -> R @get("/a") { headers: { "X-Key": string @required @auth("api_key") } }
  b(R) -> R @get("/b") { headers: { "Authorization": string @required @auth("bearer") @auth_scheme_name("X-KeyAuth") } }
}
`, `
message R {}
service API {
  a(R) -> R @get("/a") { headers: { "X-One": string @required @auth("api_key") @auth_scheme_name("Main") } }
  b(R) -> R @get("/b") { headers: { "X-Two": string @required @auth("api_key") @auth_scheme_name("Main") } }
}
`} {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), "is declared as both") {
			t.Fatalf("want conflicting scheme error, got %v", err)
		}
	}
}
