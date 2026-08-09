package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modelsOnk = `
package app.models

message User {
  id: string
  name: string @len(2, 100)
  email: string @email
}

message CreateUserRequest {
  name: string @len(2, 100)
  email: string @email
}

message GetUserRequest {
  id: string
}

message NotFoundError @status(404) {
  resource_type: string
  resource_id: string
}
`

const serviceOnk = `
package app.services

service UserService {
  base_path: "/api/v1"

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User | NotFoundError @get("/users/{id}")
}
`

const onekitToml = `
module = "example.com/testproject/api"

[generate.go-server]
out = "./api"

[generate.go-client]
out = "./api"

[generate.openapi]
out = "./docs"
title = "Test Project"
version = "1.0.0"
`

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckSucceedsOnValidProject(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "models.onk"), modelsOnk)
	writeTestFile(t, filepath.Join(dir, "service.onk"), serviceOnk)

	if err := Check(dir); err != nil {
		t.Fatalf("Check error: %v", err)
	}
}

func TestCheckFailsOnUnresolvedType(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bad.onk"), "message M {\n  x: Missing\n}\n")

	if err := Check(dir); err == nil {
		t.Fatalf("expected Check error, got nil")
	}
}

func TestLoadConfigValidatesRoutePrefix(t *testing.T) {
	for _, prefix := range []string{"api", "/", "/api/", "//api", "/api/../v1", "/api?version=1", "/api%2Fv1", "/api/{tenant}"} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			config := "module = \"example.com/api\"\nroute_prefix = \"" + prefix + "\"\n"
			writeTestFile(t, filepath.Join(dir, "onekit.toml"), config)
			if _, err := LoadConfig(dir); err == nil {
				t.Fatalf("LoadConfig unexpectedly accepted route_prefix %q", prefix)
			}
		})
	}

	dir := t.TempDir()
	config := "module = \"example.com/api\"\nroute_prefix = \"/api/internal\"\n"
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), config)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig rejected valid route_prefix: %v", err)
	}
	if cfg.RoutePrefix != "/api/internal" {
		t.Fatalf("route prefix = %q", cfg.RoutePrefix)
	}
}

func TestCheckValidatesRoutePrefixWhenConfigExists(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/api\"\nroute_prefix = \"api\"\n")
	writeTestFile(t, filepath.Join(dir, "models.onk"), modelsOnk)

	if err := Check(dir); err == nil {
		t.Fatal("Check unexpectedly accepted invalid route_prefix")
	}
}

func TestCheckAllowsLegacyContractsWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/api\"\nallow_legacy_contracts = true\n")
	writeTestFile(t, filepath.Join(dir, "models.onk"), `
message LegacyRequest {
  id: int64 @required
  from: string
}
`)

	if err := Check(dir); err != nil {
		t.Fatalf("Check rejected configured legacy contract: %v", err)
	}
}

func TestLoadConfigRejectsUnknownAndInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"unknown key", "module = \"example.com/api\"\nunknown = true\n", "unknown configuration key"},
		{"missing module", "[generate.go-server]\nout = \"./api\"\n", "module is required"},
		{"empty output", "module = \"example.com/api\"\n[generate.go-server]\nout = \"\"\n", "output path must not be empty"},
		{"route prefix without slash", "module = \"example.com/api\"\nroute_prefix = \"api\"\n", "route_prefix must start with /"},
		{"route prefix with trailing slash", "module = \"example.com/api\"\nroute_prefix = \"/api/\"\n", "route_prefix must not end with /"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "onekit.toml"), tt.config)
			_, err := LoadConfig(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestBuildGeneratesWorkingGoCode(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), onekitToml)
	writeTestFile(t, filepath.Join(dir, "models.onk"), modelsOnk)
	writeTestFile(t, filepath.Join(dir, "service.onk"), serviceOnk)

	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	apiDir := filepath.Join(dir, "api")
	for _, name := range []string{"types.gen.go", "validate.gen.go", "server.gen.go", "client.gen.go"} {
		if _, err := os.Stat(filepath.Join(apiDir, name)); err != nil {
			t.Fatalf("expected generated file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "openapi.yaml")); err != nil {
		t.Fatalf("expected generated openapi.yaml: %v", err)
	}

	writeTestFile(t, filepath.Join(apiDir, "go.mod"), "module example.com/testproject/api\n\ngo 1.26\n")
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = apiDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go package failed to build: %v\n%s", err, out)
	}
}

func TestLoadConfigRejectsSplitGoOutputs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./server"
[generate.go-client]
out = "./client"
`)
	_, err := LoadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "must use the same output path") {
		t.Fatalf("expected split Go output error, got %v", err)
	}
}

func TestBuildRemovesStaleGeneratedGroupsButPreservesUserFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./api"
`)
	writeTestFile(t, filepath.Join(dir, "old", "service.onk"), `
message Request {}
message Response {}
service API { get(Request) -> Response @get("/items") }
`)
	if err := Build(dir); err != nil {
		t.Fatalf("first Build error: %v", err)
	}
	stale := filepath.Join(dir, "api", "old", "server.gen.go")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("expected initial generated file: %v", err)
	}
	userFile := filepath.Join(dir, "api", "old", "custom.go")
	writeTestFile(t, userFile, "package old\n")
	if err := os.Remove(filepath.Join(dir, "old", "service.onk")); err != nil {
		t.Fatalf("remove schema: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "current.onk"), "message Current { id: string }\n")
	if err := Build(dir); err != nil {
		t.Fatalf("second Build error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale generated file still exists: %v", err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("user file was removed: %v", err)
	}
}
