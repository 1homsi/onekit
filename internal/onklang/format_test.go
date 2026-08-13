package onklang

import (
	"strings"
	"testing"
)

func TestFormatCanonicalizesSchemaAndPreservesDeclarationOrder(t *testing.T) {
	src := `// discarded implementation comment
package app
import "./common.onk"
/// User docs
message User @status(400){
  /// id docs
  // implementation note
  id:string?
  message: oneof(discriminator:"type"){ text:Text @tag("text") }
}
enum Status{ ACTIVE @json("active") }
service API{base_path:"/v1" get(User)->User @get("/users/{id}")}
`
	want := `// discarded implementation comment
package app

import "./common.onk"

/// User docs
message User @status(400) {
  // implementation note
  id: string?

  message: oneof(discriminator: "type") {
    text: Text @tag("text")
  }
}

enum Status {
  ACTIVE @json("active")
}

service API {
  base_path: "/v1"

  get(User) -> User @get("/users/{id}")
}
`
	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}
	if string(formatted) != want {
		t.Fatalf("formatted schema mismatch:\n got:\n%s\nwant:\n%s", formatted, want)
	}
	formattedAgain, err := Format(string(formatted))
	if err != nil {
		t.Fatalf("second Format returned error: %v", err)
	}
	if string(formattedAgain) != string(formatted) {
		t.Fatalf("format is not idempotent")
	}
}

func TestFormatRejectsInvalidSchema(t *testing.T) {
	if _, err := Format("message Broken {"); err == nil || !strings.Contains(err.Error(), "expected IDENT") {
		t.Fatalf("expected parse error, got %v", err)
	}
}
