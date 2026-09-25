package onek

import (
	"path/filepath"
	"testing"
)

func TestConfigDiagnosticsCarryPositions(t *testing.T) {
	for name, tc := range map[string]struct {
		config       string
		line, column int
	}{
		"syntax":      {"module = \"x\"\n[generate.go-server]\nout = ./api\n", 3, 7},
		"type":        {"module = \"x\"\n[generate.go-server]\nout = 5\n", 3, 1},
		"unknown key": {"module = \"x\"\n\n[generate.go-server]\n  outs = \"./a\"\n", 4, 3},
		"inline":      {"module = \"x\"\ngenerate = { go-server = { outs = \"./a\" } }\n", 2, 1},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "onekit.toml"), tc.config)
			_, err := LoadConfig(dir)
			diagnostics := Diagnostics(err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "config_error" {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			if diagnostics[0].Line != tc.line || diagnostics[0].Column != tc.column {
				t.Fatalf("position = %d:%d, want %d:%d (%s)", diagnostics[0].Line, diagnostics[0].Column, tc.line, tc.column, diagnostics[0].Message)
			}
		})
	}
}
