package onek

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestMissingConfigPointsAtOnekInit(t *testing.T) {
	_, err := LoadConfig(t.TempDir())
	if err == nil {
		t.Fatal("an empty directory has no project")
	}
	if !strings.Contains(err.Error(), "onekit.toml not found") || !strings.Contains(err.Error(), "onek init") {
		t.Fatalf("the message must say what to do: %v", err)
	}
	if strings.Contains(err.Error(), "parse ") || strings.Contains(err.Error(), "lstat") {
		t.Fatalf("a missing file is not a parse error: %v", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the cause must stay inspectable: %v", err)
	}
}
