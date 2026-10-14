package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rawRoutesOnekitToml = `
module = "example.com/raw/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts"

[generate.ts-server]
out = "./gen/ts-server"

[generate.python-client]
out = "./gen/python"

[generate.dart-client]
out = "./gen/dart"

[generate.swift-client]
out = "./gen/swift"

[generate.rust-client]
out = "./gen/rust"

[generate.rust-server]
out = "./gen/rust"

[generate.openapi]
out = "./gen/openapi"
`

const rawRoutesOnk = `
package app

message Empty {}

message Slug {
  slug: string
}

message Item {
  id: string
}

service Apps {
  base_path: "/apps"

  bundle(Slug) -> Empty @get("/{slug}/bundle.js") @http("application/javascript") @guard("app/use/:slug")
  proxy(Slug) -> Empty @any("/{slug}/proxy") @http
  info(Slug) -> Item @get("/{slug}/info")
}
`

func TestRawRoutesAreLeftOutOfTargetsThatCannotServeThem(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), rawRoutesOnekitToml)
	writeTestFile(t, filepath.Join(dir, "app", "api.onk"), rawRoutesOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("a project with raw routes and every target must build: %v", err)
	}
	read := func(parts ...string) string {
		data, err := os.ReadFile(filepath.Join(append([]string{dir}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for name, path := range map[string][]string{
		"ts-server":     {"gen", "ts-server", "app", "server.ts"},
		"python-client": {"gen", "python", "app", "client.py"},
	} {
		out := read(path...)
		if !strings.Contains(out, "/apps/{slug}/info") && !strings.Contains(out, "/apps/") {
			t.Fatalf("%s lost the typed route:\n%s", name, out)
		}
		if strings.Contains(out, "bundle.js") || strings.Contains(out, "/proxy") {
			t.Fatalf("%s generated a raw route:\n%s", name, out)
		}
	}
	for name, path := range map[string][]string{
		"go-server": {"gen", "go", "app", "server.gen.go"},
		"ts-client": {"gen", "ts", "app", "client.ts"},
	} {
		out := read(path...)
		if !strings.Contains(out, "bundle.js") || !strings.Contains(out, "/proxy") {
			t.Fatalf("%s lost the raw routes:\n%s", name, out)
		}
	}
	if !strings.Contains(read("gen", "ts", "app", "client.ts"), "export function appsBundleUrl(") {
		t.Fatal("the TypeScript client has no url builder for a raw route")
	}
	spec := read("gen", "openapi", "app", "openapi.yaml")
	for _, want := range []string{"Apps_proxy_get", "Apps_proxy_patch", "Apps_proxy_delete"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("OpenAPI lacks an operation for @any: %s\n%s", want, spec)
		}
	}
}
