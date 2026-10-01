package onkexpr_test

import (
	"encoding/json"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const fixtureSchema = `package fixture

enum Status { OPEN DONE }

message Inner {
  owner: string
  tags: string[]
}

message Subject {
  n: int32
  big: int64
  zero: int32
  d: float64
  s: string
  b: bool
  opt: string?
  optn: int32?
  status: Status
  tags: string[]
  nums: int32[]
  labels: map[string, string]
  inner: Inner
  items: Inner[]
  data: bytes
}
`

func fixture(t *testing.T) *onkir.Message {
	t.Helper()
	file, err := onklang.Parse(fixtureSchema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "fixture.onk", AST: file}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, m := range pkg.Files[0].Messages {
		if m.Name == "Subject" {
			return m
		}
	}
	t.Fatal("Subject missing")
	return nil
}

func decode(t *testing.T, m *onkir.Message, input string) map[string]any {
	t.Helper()
	var raw any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatalf("input: %v", err)
	}
	v, err := onkexpr.FromJSON(onkexpr.MessageType(m), raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v.(map[string]any)
}

type outcome int

const (
	pass outcome = iota
	fail
	evalError
)

func TestEvaluationSemantics(t *testing.T) {
	m := fixture(t)
	const base = `{"n":7,"big":"5","d":3.5,"s":"abc","b":true,"status":"OPEN","tags":["x","y"],"nums":[5,7],"labels":{"k":"v"},"inner":{"owner":"o","tags":["t"]},"items":[{"owner":"a"},{"owner":"b","tags":["q"]}],"data":"AQID"}`
	cases := []struct {
		name  string
		expr  string
		input string
		want  outcome
	}{
		{"add", "self.n + 1 == 8", base, pass},
		{"sub", "self.n - 10 == -3", base, pass},
		{"mul", "self.n * 3 == 21", base, pass},
		{"div truncates toward zero", "-7 / 2 == -3", base, pass},
		{"mod takes dividend sign", "-7 % 3 == -1", base, pass},
		{"mod positive", "7 % -3 == 1", base, pass},
		{"div by zero", "1 / self.zero == 0", base, evalError},
		{"mod by zero", "1 % self.zero == 0", base, evalError},
		{"max int64 literal", "9223372036854775807 == 9223372036854775807", base, pass},
		{"min int64 literal", "-9223372036854775808 < 0", base, pass},
		{"add overflow", `self.big + 9223372036854775807 > 0`, base, evalError},
		{"sub overflow", `-self.big - 9223372036854775807 - 9 < 0`, base, evalError},
		{"mul overflow", `self.big * 9223372036854775807 > 0`, base, evalError},
		{"negate min int64", `-self.big < 0`, `{"big":"-9223372036854775808"}`, evalError},
		{"min int64 div -1", `self.big / -1 > 0`, `{"big":"-9223372036854775808"}`, evalError},
		{"min int64 mod -1", `self.big % -1 == 0`, `{"big":"-9223372036854775808"}`, evalError},
		{"int64 near the edge is fine", `self.big + 1 > 0`, `{"big":"9223372036854775806"}`, pass},
		{"double arithmetic", "self.d * 2.0 == 7.0", base, pass},
		{"double division by zero", "self.d / 0.0 > 0.0", base, evalError},
		{"double overflow", "1e300 * 1e300 > 0.0", base, evalError},
		{"int from double truncates", "int(self.d) == 3", base, pass},
		{"int from negative double", "int(-3.99) == -3", base, pass},
		{"int from huge double", "int(1e30) == 0", base, evalError},
		{"double from int", "double(self.n) == 7.0", base, pass},
		{"double not exact", "0.1 + 0.2 == 0.3", base, fail},
		{"string size counts code points", "size(self.s) == 3", `{"s":"👍ée"}`, pass},
		{"string size of combining sequence", "size(self.s) == 2", `{"s":"é"}`, pass},
		{"equality is by scalar not canonical", "self.s == '\\u00e9'", `{"s":"é"}`, fail},
		{"equality exact", "self.s == '\\u00e9'", `{"s":"é"}`, pass},
		{"startsWith", "self.s.startsWith('ab')", base, pass},
		{"endsWith", "self.s.endsWith('bc')", base, pass},
		{"contains", "self.s.contains('b')", base, pass},
		{"contains empty", "self.s.contains('')", `{}`, pass},
		{"matches is a full match", "self.s.matches('ab')", base, fail},
		{"matches full", "self.s.matches('[a-c]+')", base, pass},
		{"matches does not treat trailing newline as end", "self.s.matches('[a-z]+')", `{"s":"abc\n"}`, fail},
		{"matches function form", "matches(self.s, 'a[^x]c')", base, pass},
		{"matches alternation", "self.s.matches('a|abc')", base, pass},
		{"matches optional group", "self.s.matches('[0-9]+(\\\\.[0-9]+)?')", `{"s":"12.5"}`, pass},
		{"ternary picks branch", "self.b ? self.n > 0 : self.n < 0", base, pass},
		{"ternary else", "self.b ? self.n < 0 : self.n > 0", `{"n":7,"b":false}`, pass},
		{"and short circuits", "false && 1 / self.zero == 0", base, fail},
		{"or short circuits", "true || 1 / self.zero == 0", base, pass},
		{"or evaluates right when left false", "false || 1 / self.zero == 0", base, evalError},
		{"and evaluates right when left true", "true && 1 / self.zero == 0", base, evalError},
		{"error on left is not masked by or", "1 / self.zero == 0 || true", base, evalError},
		{"not", "!self.b", base, fail},
		{"has optional unset", "has(self.opt)", `{}`, fail},
		{"has optional set to empty", "has(self.opt)", `{"opt":""}`, pass},
		{"has optional int set to zero", "has(self.optn)", `{"optn":0}`, pass},
		{"has string non zero", "has(self.s)", base, pass},
		{"has string empty", "has(self.s)", `{"s":""}`, fail},
		{"has int zero", "has(self.n)", `{"n":0}`, fail},
		{"has bool false", "has(self.b)", `{"b":false}`, fail},
		{"has list empty", "has(self.tags)", `{"tags":[]}`, fail},
		{"has list", "has(self.tags)", base, pass},
		{"has map empty", "has(self.labels)", `{}`, fail},
		{"has bytes", "has(self.data)", base, pass},
		{"has message unset", "has(self.inner)", `{}`, fail},
		{"has nested field", "has(self.inner.owner)", base, pass},
		{"unset reads as zero value", "self.s == '' && self.n == 0 && !self.b", `{}`, pass},
		{"unset optional reads as zero value", "self.opt == '' && self.optn == 0", `{}`, pass},
		{"unset message reads as zero value", "self.inner.owner == ''", `{}`, pass},
		{"enum equals literal", "self.status == 'OPEN'", base, pass},
		{"enum literal on left", "'OPEN' == self.status", base, pass},
		{"enum not equal", "self.status != 'DONE'", base, pass},
		{"enum in list", "self.status in ['DONE', 'OPEN']", base, pass},
		{"enum default is first value", "self.status == 'OPEN'", `{}`, pass},
		{"list size", "size(self.tags) == 2", base, pass},
		{"list size method", "self.tags.size() == 2", base, pass},
		{"list in", "'x' in self.tags", base, pass},
		{"list not in", "'z' in self.tags", base, fail},
		{"int in literal list", "self.n in [1, 7]", base, pass},
		{"index", "self.nums[1] == 7", base, pass},
		{"index out of range", "self.nums[2] == 0", base, evalError},
		{"negative index", "self.nums[-1] == 7", base, evalError},
		{"index on empty list", "self.nums[0] == 0", `{}`, evalError},
		{"all", "self.nums.all(v, v > 4)", base, pass},
		{"all fails", "self.nums.all(v, v > 5)", base, fail},
		{"all on empty is true", "self.nums.all(v, v > 100)", `{}`, pass},
		{"exists", "self.nums.exists(v, v == 7)", base, pass},
		{"exists on empty is false", "self.nums.exists(v, true)", `{}`, fail},
		{"nested macros", "self.nums.all(a, self.nums.exists(b, b >= a))", base, pass},
		{"macro over messages", "self.items.all(i, i.owner != '')", base, pass},
		{"macro over messages fails", "self.items.exists(i, size(i.tags) > 1)", base, fail},
		{"macro error propagates", "self.nums.all(v, 1 / self.zero == 0)", base, evalError},
		{"macro over empty hides error", "self.tags.all(v, 1 / self.zero == 0)", `{}`, pass},
		{"map key in", "'k' in self.labels", base, pass},
		{"map key missing", "'z' in self.labels", base, fail},
		{"map index", "self.labels['k'] == 'v'", base, pass},
		{"map missing key", "self.labels['z'] == 'v'", base, evalError},
		{"map size", "size(self.labels) == 1", base, pass},
		{"bytes size", "size(self.data) == 3", base, pass},
		{"nested message", "self.inner.owner == 'o' && size(self.inner.tags) == 1", base, pass},
		{"int64 string decodes", "self.big == 5", base, pass},
		{"int64 number decodes", "self.big == 5", `{"big":5}`, pass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := onkexpr.CompileRule(tc.expr, m, nil)
			if err != nil {
				t.Fatalf("compile %q: %v", tc.expr, err)
			}
			ok, err := onkexpr.EvalBool(node, onkexpr.Scope{"self": decode(t, m, tc.input)})
			switch tc.want {
			case pass:
				if err != nil || !ok {
					t.Fatalf("%q: want pass, got ok=%v err=%v", tc.expr, ok, err)
				}
			case fail:
				if err != nil || ok {
					t.Fatalf("%q: want fail, got ok=%v err=%v", tc.expr, ok, err)
				}
			case evalError:
				if err == nil {
					t.Fatalf("%q: want an evaluation error, got ok=%v", tc.expr, ok)
				}
			}
		})
	}
}

func compileSource(src string) (*onkir.Package, error) {
	file, err := onklang.Parse(src)
	if err != nil {
		return nil, err
	}
	return onkcompile.Compile([]onkcompile.Source{{Path: "t.onk", AST: file}})
}

func emptyMessage(t *testing.T) *onkir.Message {
	t.Helper()
	return &onkir.Message{Name: "Empty"}
}
