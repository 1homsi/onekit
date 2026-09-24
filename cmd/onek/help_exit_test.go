package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubcommandHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{{"build", "-h"}, {"fmt", "--help"}, {"mock", "-h"}, {"compat", "-h"}} {
		var stderr bytes.Buffer
		if code := exitCode(run(args), &bytes.Buffer{}, &stderr); code != 0 {
			t.Fatalf("onek %v exited %d: %s", args, code, stderr.String())
		}
		if strings.Contains(stderr.String(), "help requested") {
			t.Fatalf("onek %v printed an error: %s", args, stderr.String())
		}
	}
	if code := exitCode(run([]string{"build", "--bogus"}), &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
		t.Fatal("unknown flag exited zero")
	}
}
