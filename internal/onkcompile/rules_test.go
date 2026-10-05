package onkcompile

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func compileRules(t *testing.T, src string) error {
	t.Helper()
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, src)}})
	return err
}

func TestRulesAcceptedOnMessagesAndFields(t *testing.T) {
	err := compileRules(t, `
package api
message Span
  @rule("self.low <= self.high", "low must not exceed high")
  @rule("self.high - self.low <= 100", "range too wide")
{
  low: int32 @rule("value >= 0", "low must be non-negative")
  high: int32
}
`)
	if err != nil {
		t.Fatalf("valid rules rejected: %v", err)
	}
}

func TestRulesOnNestedMessages(t *testing.T) {
	err := compileRules(t, `
package api
message Outer {
  inner: Inner
  message Inner @rule("size(self.name) > 0", "name required") {
    name: string
  }
}
`)
	if err != nil {
		t.Fatalf("nested message rule rejected: %v", err)
	}
}

func TestRuleDiagnosticsCarryPositions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
		msg  string
	}{
		{"unknown field", "package api\nmessage M @rule(\"self.nope > 1\", \"m\") {\n  a: int32\n}\n", 2, `no field "nope"`},
		{"type mismatch", "package api\nmessage M {\n  a: int32 @rule(\"value == 'x'\", \"m\")\n}\n", 3, "one type"},
		{"not a bool", "package api\nmessage M {\n  a: int32\n}\nmessage N @rule(\"self.a\", \"m\") {\n  a: int32\n}\n", 5, "must evaluate to bool"},
		{"value outside a field rule", "package api\nmessage M @rule(\"value > 1\", \"m\") {\n  a: int32\n}\n", 2, `unknown name "value"`},
		{"unportable pattern", "package api\nmessage M {\n  a: string @rule(\"value.matches('a.c')\", \"m\")\n}\n", 3, "not portable"},
		{"unsupported field type", "package api\nmessage M {\n  a: uint64 @rule(\"value > 1\", \"m\")\n}\n", 3, "uint64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, tc.src)
			var compileErr *Error
			if !errors.As(err, &compileErr) {
				t.Fatalf("want a compile error, got %v", err)
			}
			if compileErr.Code != "invalid_rule" {
				t.Errorf("code %q, want invalid_rule", compileErr.Code)
			}
			if compileErr.Line != tc.line {
				t.Errorf("line %d, want %d (%v)", compileErr.Line, tc.line, err)
			}
			if !strings.Contains(compileErr.Msg, tc.msg) {
				t.Errorf("message %q does not contain %q", compileErr.Msg, tc.msg)
			}
		})
	}
}

func TestRuleDecoratorArguments(t *testing.T) {
	for name, src := range map[string]string{
		"missing message": "package api\nmessage M @rule(\"true\") {\n  a: int32\n}\n",
		"extra argument":  "package api\nmessage M @rule(\"true\", \"m\", \"x\") {\n  a: int32\n}\n",
		"no arguments":    "package api\nmessage M {\n  a: int32 @rule\n}\n",
		"long message":    "package api\nmessage M @rule(\"true\", \"" + strings.Repeat("x", 201) + "\") {\n  a: int32\n}\n",
	} {
		if err := compileRules(t, src); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestOtherDecoratorsStillRejectDuplicates(t *testing.T) {
	err := compileRules(t, "package api\nmessage M {\n  a: int32 @gte(1) @gte(2)\n}\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate decorator") {
		t.Fatalf("duplicate @gte should still be rejected, got %v", err)
	}
}

func TestReadmeRuleExampleCompiles(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	start := strings.Index(text, "## Validation rules")
	if start < 0 {
		t.Fatal("README has no Validation rules section")
	}
	block := text[start:]
	open := strings.Index(block, "```onk\n")
	end := strings.Index(block[open+7:], "```")
	if open < 0 || end < 0 {
		t.Fatal("README rules example not found")
	}
	if err := compileRules(t, "package api\n"+block[open+7:open+7+end]); err != nil {
		t.Fatalf("README example does not compile: %v", err)
	}
}

func TestInt64EncodingOptionMarksEveryInt64Field(t *testing.T) {
	src := `package api
message M {
  id: int64
  big: uint64
  ids: int64[]
  maybe: int64?
  text: string
  small: int32
  keep: int64 @encode("number")
  by_name: map[string, int64]
}
`
	compile := func(encoding string) map[string]bool {
		pkg, err := CompileWithOptions([]Source{{Path: "api.onk", AST: parseOrFatal(t, src)}}, CompileOptions{Int64Encoding: encoding})
		if err != nil {
			t.Fatal(err)
		}
		flagged := map[string]bool{}
		for _, f := range pkg.Files[0].Messages[0].Fields {
			flagged[f.Name] = f.Int64Number
		}
		return flagged
	}
	for _, off := range []string{"", "string"} {
		for name, flagged := range compile(off) {
			if flagged {
				t.Errorf("encoding %q must not flag %s", off, name)
			}
		}
	}
	got := compile("number")
	for _, name := range []string{"id", "big", "ids", "maybe", "keep"} {
		if !got[name] {
			t.Errorf("%s should be flagged", name)
		}
	}
	for _, name := range []string{"text", "small", "by_name"} {
		if got[name] {
			t.Errorf("%s must not be flagged", name)
		}
	}
}

func TestEmitZeroValuesOptionMarksOnlyFieldsThatAlwaysAppear(t *testing.T) {
	src := `package api
enum Level { LOW HIGH }
message Inner { v: string }
message M {
  s: string
  b: bool
  n: int32
  l: Level
  tags: string[]
  by: map[string, string]
  data: bytes
  items: Inner[]
  when: timestamp
  free: json
  maybe: string?
  inner: Inner
  pick: oneof { a: string b: int32 }
}
`
	compile := func(on bool) map[string]bool {
		pkg, err := CompileWithOptions([]Source{{Path: "api.onk", AST: parseOrFatal(t, src)}}, CompileOptions{EmitZeroValues: on})
		if err != nil {
			t.Fatal(err)
		}
		flagged := map[string]bool{}
		for _, f := range pkg.Files[0].Messages[1].Fields {
			flagged[f.Name] = f.EmitZero
		}
		return flagged
	}
	for name, flagged := range compile(false) {
		if flagged {
			t.Errorf("without the option %s must not be flagged", name)
		}
	}
	got := compile(true)
	for _, name := range []string{"s", "b", "n", "l", "tags", "by", "data", "items"} {
		if !got[name] {
			t.Errorf("%s should always be written", name)
		}
	}
	for _, name := range []string{"when", "free", "maybe", "inner", "pick"} {
		if got[name] {
			t.Errorf("%s must keep its omitted-when-unset meaning", name)
		}
	}
}
