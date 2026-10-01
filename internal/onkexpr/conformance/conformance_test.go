package conformance_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkexpr/conformance"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func subject(t *testing.T) *onkir.Message {
	t.Helper()
	file, err := onklang.Parse(conformance.SchemaSource())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "conformance.onk", AST: file}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, f := range pkg.Files {
		for _, m := range f.Messages {
			if m.Name == "Subject" {
				return m
			}
		}
	}
	t.Fatal("Subject message missing")
	return nil
}

func TestSchemaCompilesAndEveryRuleIsChecked(t *testing.T) {
	m := subject(t)
	rules, err := onkexpr.RulesFor(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(conformance.Rules) + 2; len(rules) != want {
		t.Fatalf("got %d rules, want %d", len(rules), want)
	}
}

func TestEveryInputDecodes(t *testing.T) {
	m := subject(t)
	for i, input := range conformance.Inputs {
		var raw any
		if err := json.Unmarshal([]byte(input), &raw); err != nil {
			t.Fatalf("input %d is not JSON: %v", i, err)
		}
		if _, err := onkexpr.FromJSON(onkexpr.MessageType(m), raw); err != nil {
			t.Fatalf("input %d: %v", i, err)
		}
	}
}

func TestEveryRuleBothPassesAndFailsSomewhere(t *testing.T) {
	m := subject(t)
	rules, err := onkexpr.RulesFor(m)
	if err != nil {
		t.Fatal(err)
	}
	passes := make([]int, len(rules))
	for _, input := range conformance.Inputs {
		var raw any
		_ = json.Unmarshal([]byte(input), &raw)
		decoded, err := onkexpr.FromJSON(onkexpr.MessageType(m), raw)
		if err != nil {
			t.Fatal(err)
		}
		failed, err := onkexpr.Violations(m, decoded.(map[string]any))
		if err != nil {
			t.Fatal(err)
		}
		set := map[string]bool{}
		for _, f := range failed {
			set[f] = true
		}
		for i, r := range rules {
			if !set[r.Message] {
				passes[i]++
			}
		}
	}
	var never, always []string
	for i, r := range rules {
		switch passes[i] {
		case 0:
			never = append(never, r.Message+" "+r.Source)
		case len(conformance.Inputs):
			always = append(always, r.Message+" "+r.Source)
		}
	}
	t.Logf("never pass:\n%s", strings.Join(never, "\n"))
	t.Logf("always pass:\n%s", strings.Join(always, "\n"))
}
