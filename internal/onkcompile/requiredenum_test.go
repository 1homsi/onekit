package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresOptionalMarkerForRequiredEnums(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
enum Role { ADMIN  USER }
message M { role: Role @required }
`)}})
	if err == nil || !strings.Contains(err.Error(), "@required on enum field M.role needs the ? marker") {
		t.Fatalf("want required enum error, got %v", err)
	}
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
enum Role { ADMIN  USER }
message M { role: Role? @required }
`)}}); err != nil {
		t.Fatalf("optional required enum rejected: %v", err)
	}
}
