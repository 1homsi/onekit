package onkcompat

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func compile(t *testing.T, src string) *onkcompile.Source {
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return &onkcompile.Source{Path: "api.onk", AST: ast}
}
func TestCompareFindsBreakingChanges(t *testing.T) {
	old := compile(t, `package app
message Request { id: string optional: string? }
message Response {}
service API { get(Request) -> Response @get("/items/{id}") }`)
	newer := compile(t, `package app
message Request { optional: string }
message Response {}
service API { get(Request) -> Response @get("/things") }`)
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*old})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*newer})
	if err != nil {
		t.Fatal(err)
	}
	findings := Compare(oldPkg, newPkg)
	got := make([]string, len(findings))
	for i, f := range findings {
		got[i] = f.Path + ": " + f.Message
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"app.Request.id: field was removed", "app.Request.optional: field became required", "app.API get /items/{id}: HTTP route was removed or changed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}

// TestCompareDetectsBreakingChangeDespiteCollidingEffectiveRoutes guards the
// route-snapshot keying: two services whose effective routes collide (legal
// when compile-time uniqueness is scoped by declared base_path) must not
// overwrite each other's snapshot entry, which previously made detection
// depend on file iteration order.
func TestCompareDetectsBreakingChangeDespiteCollidingEffectiveRoutes(t *testing.T) {
	old := compile(t, `package api
message R1 { id: string }
message R2 { id: string }
message R3 { id: string }

service Alpha { base_path: "/fin"
  list(R1) -> R3 @get("/users")
}

service Beta {
  list(R2) -> R2 @get("/users")
}`)
	newer := compile(t, `package api
message R1 { id: string }
message R2 { id: string }
message R3 { id: string }

service Alpha { base_path: "/fin"
  list(R2) -> R2 @get("/users")
}

service Beta {
  list(R2) -> R2 @get("/users")
}`)
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*old})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*newer})
	if err != nil {
		t.Fatal(err)
	}
	findings := Compare(oldPkg, newPkg)
	got := make([]string, len(findings))
	for i, f := range findings {
		got[i] = f.Path + ": " + f.Message
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "api.Alpha") || !strings.Contains(joined, "HTTP binding, headers, errors, or payload contract changed") {
		t.Fatalf("expected Alpha's payload change to be detected regardless of service collisions, got:\n%s", joined)
	}
}

func TestCompareDetectsOneofVariantMappingChanges(t *testing.T) {
	old := compile(t, `package app
message Envelope {
  payload: oneof {
    text: string @tag("text") @json("old_text")
  }
}`)
	newer := compile(t, `package app
message Envelope {
  payload: oneof {
    text: string @tag("text") @json("new_text")
  }
}`)
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*old})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*newer})
	if err != nil {
		t.Fatal(err)
	}
	findings := Compare(oldPkg, newPkg)
	if len(findings) != 1 || findings[0].Path != "app.Envelope.payload" {
		t.Fatalf("expected oneof mapping change finding, got %+v", findings)
	}
}
