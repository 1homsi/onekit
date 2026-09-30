package onkcompat

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
)

const requiresBase = `package app
message Req { id: string }
message Res {}
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
		`service API { get(Req) -> Res @get("/items/{id}") @requires("items:read") }`,
		`service API { get(Req) -> Res @get("/items/{id}") @requires("items:read", "items:write") }`)
	if len(findings) != 1 || findings[0].Message != "required scopes changed" ||
		findings[0].Before != "items:read" || findings[0].After != "items:read, items:write" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareReportsNewRequirementOnAnOpenRoute(t *testing.T) {
	findings := requiresFindings(t,
		`service API { get(Req) -> Res @get("/items/{id}") }`,
		`service API { get(Req) -> Res @get("/items/{id}") @requires("items:read") }`)
	if len(findings) != 1 || findings[0].Message != "required scopes changed" || findings[0].Before != "" || findings[0].After != "items:read" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareIgnoresUnchangedRequiredScopes(t *testing.T) {
	service := `service API { get(Req) -> Res @get("/items/{id}") @requires("items:read") }`
	if findings := requiresFindings(t, service, service); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}
