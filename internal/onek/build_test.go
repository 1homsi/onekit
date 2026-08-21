package onek

import (
	"encoding/json"
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
		{"route prefix with space", "module = \"example.com/api\"\nroute_prefix = \"/my api\"\n", "not allowed in a URL path"},
		{"schema root absolute", "module = \"example.com/api\"\nschema_root = \"/etc\"\n", "schema_root must be relative"},
		{"schema root escapes project", "module = \"example.com/api\"\nschema_root = \"../elsewhere\"\n", "must stay inside the project directory"},
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

func TestBuildPreservesGeneratedFilesUnderNestedTargetRoots(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./gen"
[generate.rust-client]
out = "./gen/rust"
`)
	writeTestFile(t, filepath.Join(dir, "service.onk"), `
message Request {}
message Response {}
service API { get(Request) -> Response @get("/items") }
`)

	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	for _, name := range []string{
		filepath.Join("gen", "types.gen.go"),
		filepath.Join("gen", "rust", "types.rs"),
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("nested generated output %s was removed: %v", name, err)
		}
	}
}

// TestBuildNormalizesRootBasePath pins the fix for inferred "/" base paths:
// a root-level service without base_path must generate "GET /route", not the
// malformed "GET //route" that makes net/http's ServeMux panic at startup.
func TestBuildNormalizesRootBasePath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/rootprobe\"\n\n[generate.go-server]\nout = \"./gen\"\n")
	writeTestFile(t, filepath.Join(dir, "svc.onk"), `package probe

message Req { id: string }
message Res { ok: bool }

service Svc {
  list(Req) -> Res @get("/things")
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gen", "server.gen.go"))
	if err != nil {
		t.Fatalf("read server: %v", err)
	}
	if strings.Contains(string(data), `"GET //`) || strings.Contains(string(data), "//things") && !strings.Contains(string(data), `"GET /things"`) {
		t.Fatalf("generated route contains a double slash:\n%s", data)
	}
	if !strings.Contains(string(data), `"GET /things"`) {
		t.Fatalf("expected canonical GET /things route:\n%s", data)
	}
}

// TestBuildHonorsSchemaRoot pins the voxie-style layout: onekit.toml at the
// project root, schemas under schema_root ("api"), generator outputs anchored
// at the project root. Base-path inference must follow the schema tree while
// output containment keeps being checked against the project dir.
func TestBuildHonorsSchemaRoot(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `module = "example.com/voxie/gen/go"
schema_root = "api"

[generate.go-server]
out = "gen/go"
`)
	writeTestFile(t, filepath.Join(dir, "api", "svc.onk"), `package probe

message Req { id: string }
message Res { ok: bool }

service Svc {
  list(Req) -> Res @get("/things")
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gen", "go", "server.gen.go"))
	if err != nil {
		t.Fatalf("read generated server: %v", err)
	}
	if !strings.Contains(string(data), `"GET /things"`) || strings.Contains(string(data), "//things") {
		t.Fatalf("expected canonical GET /things route from schema-root layout:\n%s", data)
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, ".onekit", "manifest.json"))
	if err != nil {
		t.Fatalf("manifest must stay at the project root: %v", err)
	}
	var manifest generationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(manifest.SchemaFiles) != 1 || manifest.SchemaFiles[0] != "svc.onk" {
		t.Fatalf("SchemaFiles should be relative to the schema root, got %+v", manifest.SchemaFiles)
	}
	if err := Check(dir); err != nil {
		t.Fatalf("Check with schema_root layout: %v", err)
	}
}

// TestCompatibilityHonorsSchemaRoot ensures onek compat compares the schema
// trees (not the project roots) when both revisions declare schema_root.
func TestCompatibilityHonorsSchemaRoot(t *testing.T) {
	configFor := `module = "example.com/compat"
schema_root = "api"
`
	schemaFor := func(fieldType string) string {
		return "package probe\n\nmessage R { id: " + fieldType + " }\nmessage Res { ok: bool }\n\nservice Svc {\n  list(R) -> Res @get(\"/things\")\n}\n"
	}
	oldDir, newDir := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(oldDir, "onekit.toml"), configFor)
	writeTestFile(t, filepath.Join(oldDir, "api", "svc.onk"), schemaFor("string"))
	writeTestFile(t, filepath.Join(newDir, "onekit.toml"), configFor)
	writeTestFile(t, filepath.Join(newDir, "api", "svc.onk"), schemaFor("int64"))

	findings, err := Compatibility(oldDir, newDir)
	if err != nil {
		t.Fatalf("Compatibility: %v", err)
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "field type") || strings.Contains(f.Message, "payload contract") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected payload-contract finding across schema_root projects, got %+v", findings)
	}
}
