package onkcompat

import "testing"

const directionBase = `namespace app;

model Req {
  id: string;
}

model User {
  name: string;
}

interface API {
  @post
  @route("/users")
  get(...Req): User;
}
`

func TestCompareAllowsNewRequiredResponseField(t *testing.T) {
	after := `namespace app;

model Req {
  id: string;
}

model User {
  name: string;
  email: string;
}

interface API {
  @post
  @route("/users")
  get(...Req): User;
}
`
	if findings := compareSchemas(t, directionBase, after); len(findings) != 0 {
		t.Fatalf("adding a response field is compatible, got %+v", findings)
	}
}

func TestCompareFlagsNewRequiredRequestField(t *testing.T) {
	after := `namespace app;

model Req {
  id: string;
  tenant: string;
}

model User {
  name: string;
}

interface API {
  @post
  @route("/users")
  get(...Req): User;
}
`
	findings := compareSchemas(t, directionBase, after)
	if len(findings) != 1 || findings[0].Message != "required field was added" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCompareReportsOptionalityOncePerDirection(t *testing.T) {
	after := `namespace app;

model Req {
  id?: string;
}

model User {
  name?: string;
}

interface API {
  @post
  @route("/users")
  get(...Req): User;
}
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
