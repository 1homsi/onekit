package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPIServersFromConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `module = "x"

[generate.openapi]
out = "./docs"
servers = ["https://api.example.com/v1", "http://localhost:8080"]
`)
	writeTestFile(t, filepath.Join(dir, "api.onk"), "message R {}\nservice S { get(R) -> R @get(\"/r\") }\n")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(string(data)), " ")
	if !strings.Contains(flat, "servers: - url: https://api.example.com/v1 - url: http://localhost:8080") {
		t.Fatalf("servers missing:\n%s", data)
	}
}
