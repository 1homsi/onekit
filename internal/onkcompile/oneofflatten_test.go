package onkcompile

import (
	"strings"
	"testing"
)

func TestFlattenedOneofRequiresMessageVariants(t *testing.T) {
	src := `package app
message Email { address: string }
message Contact {
  via: oneof(flatten: true) {
    email: Email
    phone: string
  }
}
`
	_, err := Compile([]Source{{Path: "a.onk", AST: parseOrFatal(t, src)}})
	if err == nil || !strings.Contains(err.Error(), `oneof variant "phone" must be a message to use flatten: true`) {
		t.Fatalf("err = %v", err)
	}
}
