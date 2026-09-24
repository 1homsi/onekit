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
