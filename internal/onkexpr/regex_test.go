package onkexpr_test

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

func TestPortableRegexSubset(t *testing.T) {
	accepted := conformance.Patterns
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
		{`a\-b`, "only valid inside"},
		{`[a&&b]`, "set operation"},
		{`[a||b]`, "set operation"},
		{`[a~~b]`, "set operation"},
		{`a\-b`, "only valid inside"},
		{`a\-b`, "only valid inside"},
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
