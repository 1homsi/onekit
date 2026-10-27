package onkcompat

import (
	"strings"
	"testing"
)

func TestCompareFlagsEnumValueAddedToResponse(t *testing.T) {
	schema := func(values ...string) string {
		list := strings.Join(values, ", ")
		return `namespace app;
enum Status { ` + list + ` }
enum Filter { ` + list + ` }
model Req { filter: Filter; }
model Res { status: Status; }
interface API { @post @route("/items") get(...Req): Res; }
`
	}
	findings := compareSchemas(t, schema("active", "inactive"), schema("active", "inactive", "archived"))
	if len(findings) != 1 || findings[0].Path != "app.Status.archived" {
		t.Fatalf("findings = %+v", findings)
	}
}
