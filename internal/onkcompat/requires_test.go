package onkcompat

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
)

const requiresBase = `namespace app;

model Req {
  id: string;
}

model Res {}
`

func compilePackage(t *testing.T, src string) *onkcompile.Source {
	t.Helper()
	return compile(t, src)
}

func requiresFindings(t *testing.T, oldService, newService string) []Finding {
	t.Helper()
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*compilePackage(t, requiresBase+oldService)})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*compilePackage(t, requiresBase+newService)})
	if err != nil {
		t.Fatal(err)
	}
	return Compare(oldPkg, newPkg)
}

func TestCompareReportsChangedRequiredScopes(t *testing.T) {
	findings := requiresFindings(t,
		`
interface API {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read")
  get(...Req): Res;
}
`,
		`
interface API {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read", "items:write")
  get(...Req): Res;
}
`)
	if len(findings) != 1 || findings[0].Message != "required scopes changed" ||
		findings[0].Before != "items:read" || findings[0].After != "items:read, items:write" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareReportsNewRequirementOnAnOpenRoute(t *testing.T) {
	findings := requiresFindings(t,
		`
interface API {
  @get
  @route("/items/{id}")
  get(...Req): Res;
}
`,
		`
interface API {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read")
  get(...Req): Res;
}
`)
	if len(findings) != 1 || findings[0].Message != "required scopes changed" || findings[0].Before != "" || findings[0].After != "items:read" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareIgnoresUnchangedRequiredScopes(t *testing.T) {
	service := `
interface API {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read")
  get(...Req): Res;
}
`
	if findings := requiresFindings(t, service, service); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}
