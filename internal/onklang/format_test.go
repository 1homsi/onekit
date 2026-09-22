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

// TestFormatPreservesTrailingComments guards the EOF comment pipeline: the
// lexer must hand pending comments to the EOF token so formatting keeps them
// instead of silently shrinking files.
func TestFormatPreservesTrailingComments(t *testing.T) {
	src := "package app\nmessage R { x: string }\n// license note\n"
	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(string(formatted), "// license note") {
		t.Fatalf("trailing comment was dropped:\n%s", formatted)
	}
	again, err := Format(string(formatted))
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent for trailing comments:\n%s", formatted)
	}
}

// TestFormatPreservesPackageLessCommentFiles covers notes-only schema files:
// they must format to themselves, never to empty output (which downstream
// tooling would treat as a stale generated file and delete).
func TestFormatPreservesPackageLessCommentFiles(t *testing.T) {
	for name, src := range map[string]string{
		"line comments":    "// just some notes\n",
		"block comment":    "/* scratch pad */\n",
		"doc only package": "/// docs\npackage p\n",
	} {
		formatted, err := Format(src)
		if err != nil {
			t.Fatalf("%s: format: %v", name, err)
		}
		if len(strings.TrimSpace(string(formatted))) == 0 && len(strings.TrimSpace(src)) > 0 {
			t.Fatalf("%s: non-empty source formatted to empty output", name)
		}
		if !strings.Contains(string(formatted), strings.TrimRight(strings.TrimSpace(src), "\n")) {
			t.Fatalf("%s: comment content lost:\n%s", name, formatted)
		}
	}
}

func TestFormatPreservesCommentsAboveDeclarations(t *testing.T) {
	src := `package app

// Ownership: platform team.
// Do not add fields without an ADR.
message Health {
  status: string
}

/*
  Frozen for v1.
*/
enum Level {
  LOW
}

// service-level note
service S {
  base_path: "/v1"

  get(Health) -> Health @post("/h")
}
`
	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	out := string(formatted)

	for _, want := range []string{
		"// Ownership: platform team.",
		"// Do not add fields without an ADR.",
		"Frozen for v1.",
		"// service-level note",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatting dropped %q\n--- got ---\n%s", want, out)
		}
	}

	again, err := Format(out)
	if err != nil {
		t.Fatalf("Format error on second pass: %v", err)
	}
	if string(again) != out {
		t.Errorf("Format is not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, again)
	}
}
