package onkcompat

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

func compile(t *testing.T, src string) *onkcompile.Source {
	ast, err := onktest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return &onkcompile.Source{Path: "api.tsp", AST: ast}
}
func TestCompareFindsBreakingChanges(t *testing.T) {
	old := compile(t, `namespace app;

model Request {
  id: string;
  optional?: string;
}

model Response {}

interface API {
  @get
  @route("/items/{id}")
  get(...Request): app.Response;
}
`)
	newer := compile(t, `namespace app;

model Request {
  optional: string;
}

model Response {}

interface API {
  @get
  @route("/things")
  get(...Request): app.Response;
}
`)
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
	old := compile(t, `namespace api;

model R1 {
  id: string;
}

model R2 {
  id: string;
}

model R3 {
  id: string;
}

@route("/fin")
interface Alpha {
  @get
  @route("/users")
  list(...R1): R3;
}

interface Beta {
  @get
  @route("/users")
  list(...R2): R2;
}
`)
	newer := compile(t, `namespace api;

model R1 {
  id: string;
}

model R2 {
  id: string;
}

model R3 {
  id: string;
}

@route("/fin")
interface Alpha {
  @get
  @route("/users")
  list(...R2): R2;
}

interface Beta {
  @get
  @route("/users")
  list(...R2): R2;
}
`)
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
	if !strings.Contains(joined, "api.Alpha") || !strings.Contains(joined, "api.Alpha get /fin/users: response message changed") {
		t.Fatalf("expected Alpha's payload change to be detected regardless of service collisions, got:\n%s", joined)
	}
}

func TestCompareDetectsOneofVariantMappingChanges(t *testing.T) {
	old := compile(t, `namespace app;

union EnvelopePayload {
  @Onkt.tag("text") @Onkt.json("old_text") text: string,
}

model Envelope {
  payload: EnvelopePayload;
}
`)
	newer := compile(t, `namespace app;

union EnvelopePayload {
  @Onkt.tag("text") @Onkt.json("new_text") text: string,
}

model Envelope {
  payload: EnvelopePayload;
}
`)
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
