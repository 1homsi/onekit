package main

import (
	"strings"
	"testing"
)

func TestUnknownCommandSuggestsClosest(t *testing.T) {
	for input, want := range map[string]string{"biuld": "build", "chek": "check", "lint": "check", "comp": "compat", "wach": "watch"} {
		err := run([]string{input})
		if err == nil || !strings.Contains(err.Error(), `did you mean "`+want+`"`) {
			t.Fatalf("%s: want suggestion %q, got %v", input, want, err)
		}
	}
	if err := run([]string{"zzzzzz"}); err == nil || !strings.Contains(err.Error(), "onkt help") {
		t.Fatalf("want help hint, got %v", err)
	}
	if err := run([]string{"help"}); err != nil {
		t.Fatalf("help failed: %v", err)
	}
}

func TestRemovedOnkCommandsPointAtTypeSpec(t *testing.T) {
	for command, want := range map[string]string{"fmt": "tsp format", "lsp": "TypeSpec", "mcp": "TypeSpec"} {
		err := run([]string{command})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want a pointer to %s", command, err, want)
		}
	}
}

func TestLegacyNameWarns(t *testing.T) {
	for arg0, want := range map[string]bool{"/usr/local/bin/onek": true, `C:\bin\onek.exe`: true, "onkt": false} {
		var stderr strings.Builder
		warnLegacyName(arg0, &stderr)
		if got := strings.Contains(stderr.String(), "onek is now onkt"); got != want {
			t.Errorf("%s: warned = %v, want %v", arg0, got, want)
		}
	}
}
