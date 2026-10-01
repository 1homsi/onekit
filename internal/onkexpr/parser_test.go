package onkexpr_test

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr"
)

func TestConstantExpressions(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"1 + 2 * 3 == 7", true},
		{"(1 + 2) * 3 == 9", true},
		{"10 - 3 - 2 == 5", true},
		{"100 / 10 / 5 == 2", true},
		{"-2 * -3 == 6", true},
		{"- -3 == 3", true},
		{"1 < 2 && 2 < 3", true},
		{"true || false && false", true},
		{"(true || false) && false", false},
		{"!true || true", true},
		{"!(true || true)", false},
		{"true ? false : true", false},
		{"(false ? 1 : true ? 2 : 3) == 2", true},
		{"1 + 1 == 2 ? true : false", true},
		{"-9223372036854775808 < -9223372036854775807", true},
		{"'a' == \"a\"", true},
		{"'it\\'s' == \"it's\"", true},
		{"'tab\\there' != 'tab here'", true},
		{"size('a\\nb') == 3", true},
		{"size('\\u00e9') == 1", true},
		{"size('\U0001F44D') == 1", true},
		{"size('') == 0", true},
		{"1e3 == 1000.0", true},
		{"1.5e-1 == 0.15", true},
		{"0.5 + 0.5 == 1.0", true},
		{"size([1, 2, 3]) == 3", true},
		{"2 in [1, 2, 3]", true},
		{"[1, 2][1] == 2", true},
		{"[1, 2, 3].all(x, x > 0)", true},
		{"[1, 2, 3].exists(x, x > 2)", true},
	}
	for _, tc := range cases {
		node, err := onkexpr.CompileRule(tc.expr, emptyMessage(t), nil)
		if err != nil {
			t.Errorf("%q does not compile: %v", tc.expr, err)
			continue
		}
		got, err := onkexpr.EvalBool(node, onkexpr.Scope{})
		if err != nil {
			t.Errorf("%q: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestRejectedSyntax(t *testing.T) {
	for _, expr := range []string{
		".5 + .5 == 1.0", "size([]) == 0", "1 in [1,]", "size('\\ud83d\\udc4d') == 1", "false ? 1 : true ? 2 : 3 == 2",
		"1 +* 2", "(1 + 2", "1 + 2)", "'a' 'b'", "1 2", "self", "a.b.", "[1, 2", "size(", "1 ? 2", "true ? 1 :",
		"'\\x41' == 'A'", "0x10 == 16", "1_000 == 1000", "1 = 1", "1 & 1 == 1", "1 | 1 == 1",
	} {
		if _, err := onkexpr.CompileRule(expr, emptyMessage(t), nil); err == nil {
			t.Errorf("%q should be rejected", expr)
		}
	}
}
