package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubcommandHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{{"build", "-h"}, {"mock", "-h"}, {"compat", "-h"}} {
		var stderr bytes.Buffer
		if code := exitCode(run(args), &bytes.Buffer{}, &stderr); code != 0 {
			t.Fatalf("onkt %v exited %d: %s", args, code, stderr.String())
		}
		if strings.Contains(stderr.String(), "help requested") {
			t.Fatalf("onkt %v printed an error: %s", args, stderr.String())
		}
	}
	if code := exitCode(run([]string{"build", "--bogus"}), &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
		t.Fatal("unknown flag exited zero")
	}
}

func TestMigrateWasRemovedAndPointsAtTheLastRelease(t *testing.T) {
	err := run([]string{"migrate", "--dir", "."})
	if err == nil || !strings.Contains(err.Error(), "was removed") || !strings.Contains(err.Error(), "0.39.x") {
		t.Fatalf("onkt migrate: err = %v, want a removal error with the 0.39.x hint", err)
	}
}
