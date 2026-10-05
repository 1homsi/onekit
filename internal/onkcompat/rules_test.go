package onkcompat

import (
	"fmt"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
)

func TestCompareReportsChangedMessageRules(t *testing.T) {
	findings := compareSchemas(t,
		`package app
message Span @rule("self.low <= self.high", "order") { low: int32 high: int32 }`,
		`package app
message Span @rule("self.low < self.high", "order") { low: int32 high: int32 }`)
	if len(findings) != 1 || findings[0].Path != "app.Span" || findings[0].Message != "message rules changed" {
		t.Fatalf("got %+v", findings)
	}
}

func TestCompareReportsAddedAndRemovedRules(t *testing.T) {
	bare := `package app
message Span { low: int32 high: int32 }`
	ruled := `package app
message Span @rule("self.low <= self.high", "order") { low: int32 high: int32 }`
	if findings := compareSchemas(t, bare, ruled); len(findings) != 1 {
		t.Fatalf("an added rule should be reported, got %+v", findings)
	}
	if findings := compareSchemas(t, ruled, bare); len(findings) != 1 {
		t.Fatalf("a removed rule should be reported, got %+v", findings)
	}
}

func TestCompareIgnoresRuleOrderAndReportsFieldRuleChanges(t *testing.T) {
	a := `package app
message Span @rule("self.low <= self.high", "a") @rule("self.high < 100", "b") { low: int32 @rule("value >= 0", "c") high: int32 }`
	b := `package app
message Span @rule("self.high < 100", "b") @rule("self.low <= self.high", "a") { low: int32 @rule("value >= 0", "c") high: int32 }`
	if findings := compareSchemas(t, a, b); len(findings) != 0 {
		t.Fatalf("reordering rules is not a change, got %+v", findings)
	}
	c := `package app
message Span @rule("self.high < 100", "b") @rule("self.low <= self.high", "a") { low: int32 @rule("value >= 1", "c") high: int32 }`
	findings := compareSchemas(t, b, c)
	if len(findings) != 1 || findings[0].Path != "app.Span.low" {
		t.Fatalf("a changed field rule should be reported, got %+v", findings)
	}
}

func TestCompareReportsChangedAuthorizationRules(t *testing.T) {
	base := `package app
message Principal @principal { org: string roles: string[] }
message Req { org: string }
message Resp { ok: bool }
service S { get(Req) -> Resp @get("/x") %s }`
	allow := func(rule string) string { return fmt.Sprintf(base, rule) }
	loose := allow(`@authorize("auth.org == req.org", "wrong org")`)
	strict := allow(`@authorize("auth.org == req.org && 'admin' in auth.roles", "wrong org")`)
	for name, pair := range map[string][2]string{
		"changed": {loose, strict},
		"added":   {allow(""), loose},
		"removed": {loose, allow("")},
	} {
		findings := compareSchemas(t, pair[0], pair[1])
		if len(findings) != 1 || findings[0].Message != "authorization rules changed" {
			t.Fatalf("%s: got %+v", name, findings)
		}
	}
	if findings := compareSchemas(t, loose, loose); len(findings) != 0 {
		t.Fatalf("an unchanged rule is not a change: %+v", findings)
	}
}

func TestCompareReportsChangingTheProjectInt64Encoding(t *testing.T) {
	src := `package app
message Item { id: int64 ids: int64[] name: string }
`
	build := func(encoding string) *onkir.Package {
		pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{*compile(t, src)}, onkcompile.CompileOptions{Int64Encoding: encoding})
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	findings := Compare(build(""), build("number"))
	paths := map[string]bool{}
	for _, f := range findings {
		paths[f.Path] = true
	}
	if !paths["app.Item.id"] || !paths["app.Item.ids"] || paths["app.Item.name"] {
		t.Fatalf("changing the encoding must flag exactly the int64 fields, got %+v", findings)
	}
	if len(Compare(build("number"), build("number"))) != 0 {
		t.Fatal("an unchanged encoding is not a change")
	}
}

func TestCompareReportsChangedRouteMetadata(t *testing.T) {
	base := `package app
message R { id: string }
service S { get(R) -> R @get("/x/{id}") %s }`
	with := func(meta string) string { return fmt.Sprintf(base, meta) }
	findings := compareSchemas(t, with(`@meta("guard", "app/use/:slug")`), with(`@meta("guard", "app/edit/:slug")`))
	if len(findings) != 1 || findings[0].Message != "route metadata changed" {
		t.Fatalf("changed: %+v", findings)
	}
	if len(compareSchemas(t, with(""), with(`@meta("audit", "x")`))) != 1 || len(compareSchemas(t, with(`@meta("audit", "x")`), with(""))) != 1 {
		t.Fatal("added and removed metadata must be reported")
	}
	if len(compareSchemas(t, with(`@meta("a", "1") @meta("b", "2")`), with(`@meta("a", "1") @meta("b", "2")`))) != 0 {
		t.Fatal("unchanged metadata is not a change")
	}
}
