package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresOptionalMarkerForRequiredEnums(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
enum Role {
  ADMIN,
  USER,
}

model M {
  @Onkt.required
  role: Role;
}
`)}})
	if err == nil || !strings.Contains(err.Error(), "@required on enum field M.role needs the ? marker") {
		t.Fatalf("want required enum error, got %v", err)
	}
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
enum Role {
  ADMIN,
  USER,
}

model M {
  @Onkt.required
  role?: Role;
}
`)}}); err != nil {
		t.Fatalf("optional required enum rejected: %v", err)
	}
}

func TestCompileAllowsRequiredEnumsInLegacyContracts(t *testing.T) {
	source := Source{Path: "api.tsp", AST: parseOrFatal(t, `
enum Role {
  ADMIN,
  USER,
}

model M {
  @Onkt.required
  role: Role;
}
`)}
	if _, err := CompileWithOptions([]Source{source}, CompileOptions{AllowLegacyContracts: true}); err != nil {
		t.Fatalf("legacy contract rejected: %v", err)
	}
}
