package onkcompile

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

func compileRules(t *testing.T, src string) error {
	t.Helper()
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, src)}})
	return err
}

func TestRulesAcceptedOnMessagesAndFields(t *testing.T) {
	err := compileRules(t, `namespace api;

@Onkt.rule("self.low <= self.high", "low must not exceed high")
@Onkt.rule("self.high - self.low <= 100", "range too wide")
model Span {
  @Onkt.rule("value >= 0", "low must be non-negative")
  low: int32;
  high: int32;
}
`)
	if err != nil {
		t.Fatalf("valid rules rejected: %v", err)
	}
}

func TestRulesOnNestedMessages(t *testing.T) {
	err := compileRules(t, `namespace api;

model Outer {
  inner: Inner;
}

@Onkt.rule("size(self.name) > 0", "name required")
@Onkt.parent(Outer)
model Inner {
  name: string;
}
`)
	if err != nil {
		t.Fatalf("nested message rule rejected: %v", err)
	}
}

// A message rule reports the line of its @Onkt.rule decorator, a field rule
// the line of its field.
func TestRuleDiagnosticsCarryPositions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
		msg  string
	}{
		{"unknown field", `namespace api;

@Onkt.rule("self.nope > 1", "m")
model M {
  a: int32;
}
`, 3, `no field "nope"`},
		{"type mismatch", `namespace api;

model M {
  @Onkt.rule("value == 'x'", "m")
  a: int32;
}
`, 5, "one type"},
		{"not a bool", `namespace api;

model M {
  a: int32;
}

@Onkt.rule("self.a", "m")
model N {
  a: int32;
}
`, 7, "must evaluate to bool"},
		{"value outside a field rule", `namespace api;

@Onkt.rule("value > 1", "m")
model M {
  a: int32;
}
`, 3, `unknown name "value"`},
		{"unportable pattern", `namespace api;

model M {
  @Onkt.rule("value.matches('a.c')", "m")
  a: string;
}
`, 5, "not portable"},
		{"unsupported field type", `namespace api;

model M {
  @Onkt.rule("value > 1", "m")
  @encode(string)
  a: uint64;
}
`, 6, "uint64"},
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

// TypeSpec's signature for @Onkt.rule already requires exactly an
// expression and a message; documents from any frontend are checked too.
func TestRuleDecoratorArguments(t *testing.T) {
	q, dec, field := onktest.Q, onktest.Dec, onktest.Field
	for name, file := range map[string]*onkschema.File{
		"missing message": onktest.File("api", onktest.Message("M", field("a", "int32"), dec("rule", q("true")))),
		"extra argument":  onktest.File("api", onktest.Message("M", field("a", "int32"), dec("rule", q("true"), q("m"), q("x")))),
		"no arguments":    onktest.File("api", onktest.Message("M", field("a", "int32", dec("rule")))),
	} {
		if _, err := Compile([]Source{{Path: "api.tsp", AST: file}}); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
	long := "namespace api;\n@Onkt.rule(\"true\", \"" + strings.Repeat("x", 201) + "\")\nmodel M {\n  a: int32;\n}\n"
	if err := compileRules(t, long); err == nil {
		t.Error("long message: should be rejected")
	}
}

func TestOtherDecoratorsStillRejectDuplicates(t *testing.T) {
	err := compileRules(t, `namespace api;

model M {
  @minValue(1)
  @minValue(2)
  a: int32;
}
`)
	if err == nil || !strings.Contains(err.Error(), "duplicate decorator") {
		t.Fatalf("duplicate @gte should still be rejected, got %v", err)
	}
}

func TestInt64EncodingOptionMarksEveryInt64Field(t *testing.T) {
	src := `namespace api;

model M {
  @encode(string)
  id: int64;
  @encode(string)
  big: uint64;
  @Onkt.encode("string")
  ids: int64[];
  @encode(string)
  maybe?: int64;
  text: string;
  small: int32;
  keep: int64;
  by_name: Record<int64>;
}
`
	compile := func(encoding string) map[string]bool {
		pkg, err := CompileWithOptions([]Source{{Path: "api.tsp", AST: parseOrFatal(t, src)}}, CompileOptions{Int64Encoding: encoding})
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
	src := `namespace api;

enum Level {
  LOW,
  HIGH,
}

model Inner {
  v: string;
}

union MPick {
  x: string,
  y: int32,
}

model M {
  s: string;
  b: boolean;
  n: int32;
  l: Level;
  tags: string[];
  by: Record<string>;
  data: bytes;
  items: Inner[];
  when: utcDateTime;
  free: unknown;
  maybe?: string;
  inner: Inner;
  pick: MPick;
}
`
	compile := func(on bool) map[string]bool {
		pkg, err := CompileWithOptions([]Source{{Path: "api.tsp", AST: parseOrFatal(t, src)}}, CompileOptions{EmitZeroValues: on})
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

func TestMetaDecoratorValidation(t *testing.T) {
	const base = "namespace api;\nmodel R { id: string; }\ninterface S {\n  @get @route(\"/r/{id}\") %s get(...R): R;\n}\n"
	ok := func(decorators string) error { return compileRules(t, fmt.Sprintf(base, decorators)) }
	if err := ok(`@Onkt.meta("guard", "app/edit/:slug") @Onkt.meta("audit.event", "app.update")`); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	for name, decorators := range map[string]string{
		"upper case key":   `@Onkt.meta("Guard", "x")`,
		"key with a space": `@Onkt.meta("my key", "x")`,
		"digit first":      `@Onkt.meta("1x", "x")`,
		"empty value":      `@Onkt.meta("guard", "")`,
		"long value":       `@Onkt.meta("guard", "` + strings.Repeat("x", 201) + `")`,
		"duplicate key":    `@Onkt.meta("guard", "a") @Onkt.meta("guard", "b")`,
	} {
		err := ok(decorators)
		var compileErr *Error
		if !errors.As(err, &compileErr) || compileErr.Code != "invalid_meta" {
			t.Errorf("%s: want an invalid_meta error, got %v", name, err)
		}
	}
	// TypeSpec's signature already requires a key and a value; documents
	// with another count are refused too.
	q, dec := onktest.Q, onktest.Dec
	for name, meta := range map[string]onkschema.Decorator{
		"one argument":    dec("meta", q("guard")),
		"three arguments": dec("meta", q("a"), q("b"), q("c")),
	} {
		file := onktest.File("api", onktest.Message("R", onktest.Field("id", "string")),
			onktest.Service("S", "", onktest.RPC("get", "R", "R", dec("get", q("/r/{id}")), meta)))
		_, err := Compile([]Source{{Path: "api.tsp", AST: file}})
		var compileErr *Error
		if !errors.As(err, &compileErr) || compileErr.Code != "invalid_meta" {
			t.Errorf("%s: want an invalid_meta error, got %v", name, err)
		}
	}
	ws := `namespace api;

model R {
  id: string;
}

interface S {
  @Onkt.ws
  @route("/ws")
  @Onkt.meta("a", "b")
  stream(...R): R;
}
`
	if err := compileRules(t, ws); err == nil || !strings.Contains(err.Error(), "@meta") {
		t.Errorf("@meta on a WebSocket method should be rejected, got %v", err)
	}
}

func TestRepeatedInt64CanBeEncodedAsNumbers(t *testing.T) {
	src := `namespace api;

model M {
  ids: int64[];
  @Onkt.encode("string")
  keys: uint64[];
}
`
	ast, err := onktest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "/p/api.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	fields := pkg.Files[0].Messages[0].Fields
	if !fields[0].Int64Number || len(fields[0].Decorators) != 0 {
		t.Fatalf("ids = %+v, want numeric items and no @encode left", fields[0])
	}
	if fields[1].Int64Number {
		t.Fatal("a repeated int64 without @encode keeps the project default")
	}
}
