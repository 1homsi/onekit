package onkexpr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr"
)

func TestDiagnosticsPointAtTheOffendingToken(t *testing.T) {
	m := fixture(t)
	cases := []struct {
		expr string
		at   string
		msg  string
	}{
		{"self.n +", "", "unexpected end of expression"},
		{"1 < 2 < 3", "< 3", "cannot be chained"},
		{"self.nope == 1", "nope", `no field "nope"`},
		{"self.n == 'a'", "==", "one type"},
		{"self.n + 1.0 > 0.0", "+", "double(x) or int(x)"},
		{"self.s + 'x' == 'y'", "+", "two ints or two doubles"},
		{"self.s < 'b'", "<", "compares two ints or two doubles"},
		{"self.n", "n", "must evaluate to bool"},
		{"size(self.n) == 1", "size", "strings, bytes, lists and maps"},
		{"has(self.n + 1)", "has", "needs a field"},
		{"self.status == 'NOPE'", "'NOPE'", "not a value of Status"},
		{"matches(self.s, self.s)", "s)", "string literal"},
		{"self.s.matches('(a+)+')", "'(a+)+'", "nested repetition"},
		{"unknown(1)", "unknown", "unknown function"},
		{"self.tags.all(t, t)", "t)", "must be bool"},
		{"x == 1", "x", `unknown name "x"`},
		{"self.b ? 1 : 'a'", "?", "one type"},
		{"size(self.b ? self.tags : self.tags) == 0", "?", "must be bool, int, double, string or an enum"},
		{"self.n == 99999999999999999999", "99999999999999999999", "does not fit in 64 bits"},
		{"self.data == self.data", "==", "cannot compare bytes"},
		{"self.data[0] == 1", "[", "cannot index"},
		{"self.n.foo", "foo", "cannot select"},
		{"self.nums['a']", "'a'", "must be an int"},
		{`"unterminated`, `"`, "unterminated string"},
		{"1 $ 2", "$", "unexpected character"},
		{"value > 1", "value", `unknown name "value"`},
		{"self.n > 1 && self.nums.all(v, v)", "v)", "must be bool"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := onkexpr.CompileRule(tc.expr, m, nil)
			var exprErr *onkexpr.Error
			if !errors.As(err, &exprErr) {
				t.Fatalf("want an expression error, got %v", err)
			}
			if !strings.Contains(exprErr.Message, tc.msg) {
				t.Errorf("message %q does not contain %q", exprErr.Message, tc.msg)
			}
			want := len(tc.expr)
			if tc.at != "" {
				want = strings.Index(tc.expr, tc.at)
			}
			if exprErr.Offset != want {
				t.Errorf("offset %d, want %d (%q)", exprErr.Offset, want, tc.at)
			}
		})
	}
}

func TestExpressionLimits(t *testing.T) {
	m := fixture(t)
	long := "self.n == 1" + strings.Repeat(" || self.n == 1", 80)
	if _, err := onkexpr.CompileRule(long, m, nil); err == nil || !strings.Contains(err.Error(), "1024") {
		t.Errorf("an expression over 1024 bytes should be rejected, got %v", err)
	}
	deep := strings.Repeat("(", 60) + "true" + strings.Repeat(")", 60)
	if _, err := onkexpr.CompileRule(deep, m, nil); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Errorf("deep nesting should be rejected, got %v", err)
	}
	ok := strings.Repeat("(", 20) + "true" + strings.Repeat(")", 20)
	if _, err := onkexpr.CompileRule(ok, m, nil); err != nil {
		t.Errorf("moderate nesting should compile: %v", err)
	}
}

func TestRejectedFieldTypes(t *testing.T) {
	for _, field := range []string{"u: uint64", "ts: timestamp", "j: json"} {
		file := "package p\nmessage M @rule(\"true\", \"ok\") {\n  " + field + " @rule(\"true\", \"ok\")\n}\n"
		_, err := compileSource(file)
		if err == nil {
			t.Errorf("%s: a field rule on this type should be rejected", field)
		}
	}
}
