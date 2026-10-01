package onkexpr_test

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr"
)

func TestPortableRegexSubset(t *testing.T) {
	accepted := []string{
		`abc`, `[a-z]+`, `[A-Za-z0-9_]*`, `[^a]+`, `a|b|c`, `(ab)+`, `(?:ab)*c?`, `[0-9]{3}`, `[0-9]{2,4}`,
		`[0-9]+(\.[0-9]+)?`, `[a-z]+(-[a-z]+)?`, `(ab|cd)?x`, `[à-ÿ]+`, `a{0,3}`, `\\`, `\n`, `[\]]`, `\+\*\?`, `x{1000}`,
		`(a?)b`, `[a-z]{1,63}`,
	}
	for _, re := range accepted {
		if err := onkexpr.ValidateRegex(re); err != nil {
			t.Errorf("%q should be portable: %v", re, err)
		}
	}
	rejected := []struct{ re, why string }{
		{``, "empty"},
		{`a.c`, "'.'"},
		{`^a`, "anchors"},
		{`a$`, "anchors"},
		{`\d+`, "not portable"},
		{`\w`, "not portable"},
		{`\b`, "not portable"},
		{`(a+)+`, "nested repetition"},
		{`(a*)*`, "nested repetition"},
		{`(a+){2,3}`, "nested repetition"},
		{`a**`, "nested repetition"},
		{`(a|b)+`, "'|'"},
		{`(ab|cd)*`, "'|'"},
		{`a+?`, "lazy"},
		{`a++`, "possessive"},
		{`(?=a)`, "(?:"},
		{`(?<n>a)`, "(?:"},
		{`(?i)a`, "(?:"},
		{`a{2,}`, "open-ended"},
		{`a{3,2}`, "bounds"},
		{`a{1001}`, "bounds"},
		{`a{,3}`, "number"},
		{`(a`, "missing"},
		{`a)`, "unmatched"},
		{`[a`, "missing ']'"},
		{`[[:alpha:]]`, "POSIX"},
		{`[a-]`, "literal '-'"},
		{`[z-a]`, "range"},
		{`a||b`, "empty alternative"},
		{`|a`, "empty alternative"},
		{`()`, "empty"},
		{strings.Repeat("a", 257), "256"},
		{strings.Repeat("(", 9) + "a" + strings.Repeat(")", 9), "deeply"},
	}
	for _, tc := range rejected {
		err := onkexpr.ValidateRegex(tc.re)
		if err == nil {
			t.Errorf("%q should be rejected", tc.re)
			continue
		}
		if !strings.Contains(err.Error(), tc.why) {
			t.Errorf("%q: error %q does not mention %q", tc.re, err, tc.why)
		}
	}
}
