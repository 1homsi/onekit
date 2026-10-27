package onek

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitIgnoresManifestDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || string(data) != ".onekit/\nnode_modules/\n" {
		t.Fatalf("unexpected .gitignore %q: %v", data, err)
	}

	existing := t.TempDir()
	writeTestFile(t, filepath.Join(existing, ".gitignore"), "node_modules")
	if err := Init(existing, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := Init(existing, true); err != nil {
		t.Fatalf("Init --force: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(existing, ".gitignore"))
	// "node_modules" already ignores node_modules/, so it is not added twice.
	if string(data) != "node_modules\n.onekit/\n" {
		t.Fatalf("existing .gitignore not preserved and extended once: %q", data)
	}
}
