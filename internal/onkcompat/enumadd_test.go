package onkcompat

import "testing"

func TestCompareFlagsEnumValueAddedToResponse(t *testing.T) {
	schema := func(values string) string {
		return `package app
enum Status { ` + values + ` }
enum Filter { ` + values + ` }
message Req { filter: Filter }
message Res { status: Status }
service API { get(Req) -> Res @post("/items") }
`
	}
	findings := compareSchemas(t, schema("active inactive"), schema("active inactive archived"))
	if len(findings) != 1 || findings[0].Path != "app.Status.archived" {
		t.Fatalf("findings = %+v", findings)
	}
}
