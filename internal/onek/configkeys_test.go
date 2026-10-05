package onek

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigSuggestsKnownKeys(t *testing.T) {
	tests := map[string]string{
		"module = \"x\"\n[generate.python]\nout = \"./py\"\n":               "generate.python (did you mean generate.python-client?)",
		"module = \"x\"\n[generate.go-server]\nout = \"./a\"\nzod = true\n": "generate.go-server.zod",
		"modul = \"x\"\n":                          "modul (did you mean module?)",
		"module = \"x\"\nroute_prefx = \"/api\"\n": "route_prefx (did you mean route_prefix?)",
	}
	for config, want := range tests {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "onekit.toml"), config)
		_, err := LoadConfig(dir)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("config %q: want %q, got %v", config, want, err)
		}
		if strings.Contains(err.Error(), "generate.python.out") {
			t.Fatalf("child of an unknown table reported: %v", err)
		}
	}
}

func TestLoadConfigValidatesInt64Encoding(t *testing.T) {
	for value, ok := range map[string]bool{"": true, "string": true, "number": true, "float": false} {
		dir := t.TempDir()
		config := "module = \"x\"\n[generate.go-server]\nout = \"./a\"\n"
		if value != "" {
			config = "module = \"x\"\nint64_encoding = \"" + value + "\"\n[generate.go-server]\nout = \"./a\"\n"
		}
		writeTestFile(t, filepath.Join(dir, "onekit.toml"), config)
		cfg, err := LoadConfig(dir)
		if ok && err != nil {
			t.Errorf("%q should load: %v", value, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "int64_encoding")) {
			t.Errorf("%q should be rejected with a clear message, got %v", value, err)
		}
		if ok && cfg.CompileOptions().Int64Encoding != value {
			t.Errorf("%q not passed to the compiler: %+v", value, cfg.CompileOptions())
		}
	}
}
