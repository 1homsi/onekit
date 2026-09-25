package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatAcceptsFilesAndStdin(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.onk")
	b := filepath.Join(dir, "b.onk")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("message X{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"fmt", a}); err != nil {
		t.Fatalf("fmt file: %v", err)
	}
	if data, _ := os.ReadFile(a); string(data) != "message X {\n}\n" {
		t.Fatalf("a.onk not formatted: %q", data)
	}
	if data, _ := os.ReadFile(b); string(data) != "message X{}\n" {
		t.Fatalf("b.onk touched: %q", data)
	}
	var out bytes.Buffer
	if err := formatStdin(strings.NewReader("message Y{}\n"), &out); err != nil || out.String() != "message Y {\n}\n" {
		t.Fatalf("stdin: %q %v", out.String(), err)
	}
}
