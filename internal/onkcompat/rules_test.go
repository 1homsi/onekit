package onkcompat

import "testing"

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
