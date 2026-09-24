package main

import (
	"strings"
	"testing"
)

func TestUnknownCommandSuggestsClosest(t *testing.T) {
	for input, want := range map[string]string{"biuld": "build", "chek": "check", "format": "fmt", "lint": "check", "comp": "compat", "wach": "watch"} {
		err := run([]string{input})
		if err == nil || !strings.Contains(err.Error(), `did you mean "`+want+`"`) {
			t.Fatalf("%s: want suggestion %q, got %v", input, want, err)
		}
	}
	if err := run([]string{"zzzzzz"}); err == nil || !strings.Contains(err.Error(), "onek help") {
		t.Fatalf("want help hint, got %v", err)
	}
	if err := run([]string{"help"}); err != nil {
		t.Fatalf("help failed: %v", err)
	}
}
