package onkcompat

import "testing"

const directionBase = `package app
message Req { id: string }
message User { name: string }
service API { get(Req) -> User @get("/users") }
`

func TestCompareAllowsNewRequiredResponseField(t *testing.T) {
	after := `package app
message Req { id: string }
message User { name: string email: string }
service API { get(Req) -> User @get("/users") }
`
	if findings := compareSchemas(t, directionBase, after); len(findings) != 0 {
		t.Fatalf("adding a response field is compatible, got %+v", findings)
	}
}

func TestCompareFlagsNewRequiredRequestField(t *testing.T) {
	after := `package app
message Req { id: string tenant: string }
message User { name: string }
service API { get(Req) -> User @get("/users") }
`
	findings := compareSchemas(t, directionBase, after)
	if len(findings) != 1 || findings[0].Message != "required field was added" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareReportsOptionalityOncePerDirection(t *testing.T) {
	after := `package app
message Req { id: string? }
message User { name: string? }
service API { get(Req) -> User @get("/users") }
`
	findings := compareSchemas(t, directionBase, after)
	if len(findings) != 1 || findings[0].Path != "app.User.name" || findings[0].Message != "field became optional in a response" {
		t.Fatalf("findings = %+v", findings)
	}
	findings = compareSchemas(t, after, directionBase)
	if len(findings) != 1 || findings[0].Path != "app.Req.id" || findings[0].Message != "field became required" {
		t.Fatalf("findings = %+v", findings)
	}
}
