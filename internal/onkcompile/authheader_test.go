package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresAuthorizationHeaderForHTTPAuth(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R {}
service API {
  headers: { "X-Token": string @required @auth("bearer") }
  a(R) -> R @get("/x")
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "must be declared on the Authorization header") {
		t.Fatalf("want Authorization header error, got %v", err)
	}
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R {}
service API {
  headers: {
    "authorization": string @required @auth("basic")
    "X-Key": string @required @auth("api_key")
  }
  a(R) -> R @get("/x")
}
`)}}); err != nil {
		t.Fatalf("valid auth headers rejected: %v", err)
	}
}
