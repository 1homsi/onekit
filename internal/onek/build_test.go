package onek

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modelsTypeSpec = `namespace app.models;

model User {
  id: string;
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
}

model CreateUserRequest {
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
}

model GetUserRequest {
  id: string;
}

@error
model NotFoundError {
  @statusCode _: 404;
  resource_type: string;
  resource_id: string;
}
`

const serviceTypeSpec = `using app.models;

namespace app.services;

@route("/api/v1")
interface UserService {
  @post
  @route("/users")
  createUser(...CreateUserRequest): User;

  @get
  @route("/users/{id}")
  getUser(...GetUserRequest): User | NotFoundError;
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
	writeTestFile(t, filepath.Join(dir, "models.tsp"), modelsTypeSpec)
	writeTestFile(t, filepath.Join(dir, "service.tsp"), serviceTypeSpec)

	if err := Check(dir); err != nil {
		t.Fatalf("Check error: %v", err)
	}
}

func TestCheckFailsOnUnresolvedType(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bad.tsp"), `
model M {
  x: Missing;
}
`)

	if err := Check(dir); err == nil {
		t.Fatalf("expected Check error, got nil")
	}
}

func TestLoadConfigValidatesRoutePrefix(t *testing.T) {
	for _, prefix := range []string{"api", "/", "/api/", "//api", "/api/../v1", "/api?version=1", "/api%2Fv1", "/api/{tenant}"} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			config := "module = \"example.com/api\"\nroute_prefix = \"" + prefix + "\"\n"
			writeTestFile(t, filepath.Join(dir, "onkt.toml"), config)
			if _, err := LoadConfig(dir); err == nil {
				t.Fatalf("LoadConfig unexpectedly accepted route_prefix %q", prefix)
			}
		})
	}

	dir := t.TempDir()
	config := "module = \"example.com/api\"\nroute_prefix = \"/api/internal\"\n"
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), config)
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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/api\"\nroute_prefix = \"api\"\n")
	writeTestFile(t, filepath.Join(dir, "models.tsp"), modelsTypeSpec)

	if err := Check(dir); err == nil {
		t.Fatal("Check unexpectedly accepted invalid route_prefix")
	}
}

func TestCheckAllowsLegacyContractsWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/api\"\nallow_legacy_contracts = true\n")
	writeTestFile(t, filepath.Join(dir, "models.tsp"), `
model LegacyRequest {
  @Onkt.required
  @encode(string)
  id: int64;
  from: string;
}
`)

	if err := Check(dir); err != nil {
		t.Fatalf("Check rejected configured legacy contract: %v", err)
	}
}

func TestLoadConfigRejectsUnknownAndInvalidConfiguration(t *testing.T) {
	// filepath.IsAbs requires a drive letter or UNC prefix on Windows, so a
	// bare "/etc"-style literal isn't absolute there - build one that's
	// actually absolute on whichever OS the test runs on.
	absoluteSchemaRoot := filepath.ToSlash(t.TempDir())

	tests := []struct {
		name, config, want string
	}{
		{"unknown key", "module = \"example.com/api\"\nunknown = true\n", "unknown configuration key"},
		{"missing module", "[generate.go-server]\nout = \"./api\"\n", "module is required"},
		{"empty output", "module = \"example.com/api\"\n[generate.go-server]\nout = \"\"\n", "output path must not be empty"},
		{"route prefix without slash", "module = \"example.com/api\"\nroute_prefix = \"api\"\n", "route_prefix must start with /"},
		{"route prefix with trailing slash", "module = \"example.com/api\"\nroute_prefix = \"/api/\"\n", "route_prefix must not end with /"},
		{"route prefix with space", "module = \"example.com/api\"\nroute_prefix = \"/my api\"\n", "not allowed in a URL path"},
		{"schema root absolute", "module = \"example.com/api\"\nschema_root = \"" + absoluteSchemaRoot + "\"\n", "schema_root must be relative"},
		{"schema root escapes project", "module = \"example.com/api\"\nschema_root = \"../elsewhere\"\n", "must stay inside the project directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "onkt.toml"), tt.config)
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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), onekitToml)
	writeTestFile(t, filepath.Join(dir, "models.tsp"), modelsTypeSpec)
	writeTestFile(t, filepath.Join(dir, "service.tsp"), serviceTypeSpec)

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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `
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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./api"
`)
	writeTestFile(t, filepath.Join(dir, "old", "service.tsp"), `
model Request {}

model Response {}

interface API {
  @get
  @route("/items")
  get(...Request): global.Response;
}
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
	if err := os.Remove(filepath.Join(dir, "old", "service.tsp")); err != nil {
		t.Fatalf("remove schema: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "current.tsp"), `
model Current {
  id: string;
}
`)
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

func TestBuildKeepsAHandWrittenFileAtAFormerlyGeneratedPath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./api"
`)
	writeTestFile(t, filepath.Join(dir, "old", "service.tsp"), `
model Request {}

model Response {}

interface API {
  @get
  @route("/items")
  get(...Request): global.Response;
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("first Build error: %v", err)
	}
	generated := filepath.Join(dir, "api", "old", "server.gen.go")
	handWritten := filepath.Join(dir, "api", "old", "types.gen.go")
	writeTestFile(t, handWritten, "// my own file, kept at the old generated path\npackage old\n")
	if err := os.Remove(filepath.Join(dir, "old", "service.tsp")); err != nil {
		t.Fatalf("remove schema: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "current.tsp"), `
model Current {
  id: string;
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("second Build error: %v", err)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("a stale generated file must still be removed: %v", err)
	}
	data, err := os.ReadFile(handWritten)
	if err != nil || !strings.Contains(string(data), "my own file") {
		t.Fatalf("a file without the generated banner must be left alone: %v %q", err, data)
	}
}

func TestBuildPreservesGeneratedFilesUnderNestedTargetRoots(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `
module = "example.com/api"
[generate.go-server]
out = "./gen"
[generate.rust-client]
out = "./gen/rust"
`)
	writeTestFile(t, filepath.Join(dir, "service.tsp"), `
model Request {}

model Response {}

interface API {
  @get
  @route("/items")
  get(...Request): global.Response;
}
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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/rootprobe\"\n\n[generate.go-server]\nout = \"./gen\"\n")
	writeTestFile(t, filepath.Join(dir, "svc.tsp"), `namespace probe;

model Req {
  id: string;
}

model Res {
  ok: boolean;
}

interface Svc {
  @get
  @route("/things")
  list(...Req): Res;
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

// TestBuildHonorsSchemaRoot pins the voxie-style layout: onkt.toml at the
// project root, schemas under schema_root ("api"), generator outputs anchored
// at the project root. Base-path inference must follow the schema tree while
// output containment keeps being checked against the project dir.
func TestBuildHonorsSchemaRoot(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `module = "example.com/voxie/gen/go"
schema_root = "api"

[generate.go-server]
out = "gen/go"
`)
	writeTestFile(t, filepath.Join(dir, "api", "svc.tsp"), `namespace probe;

model Req {
  id: string;
}

model Res {
  ok: boolean;
}

interface Svc {
  @get
  @route("/things")
  list(...Req): Res;
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
	if len(manifest.SchemaFiles) != 1 || manifest.SchemaFiles[0] != "svc.tsp" {
		t.Fatalf("SchemaFiles should be relative to the schema root, got %+v", manifest.SchemaFiles)
	}
	if err := Check(dir); err != nil {
		t.Fatalf("Check with schema_root layout: %v", err)
	}
}

// TestCompatibilityHonorsSchemaRoot ensures onkt compat compares the schema
// trees (not the project roots) when both revisions declare schema_root.
func TestCompatibilityHonorsSchemaRoot(t *testing.T) {
	configFor := `module = "example.com/compat"
schema_root = "api"
`
	schemaFor := func(fieldType string) string {
		return "namespace probe;\n\nmodel R { id: " + fieldType + "; }\nmodel Res { ok: boolean; }\n\ninterface Svc {\n  @get @route(\"/things\") list(...R): Res;\n}\n"
	}
	oldDir, newDir := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(oldDir, "onkt.toml"), configFor)
	writeTestFile(t, filepath.Join(oldDir, "api", "svc.tsp"), schemaFor("string"))
	writeTestFile(t, filepath.Join(newDir, "onkt.toml"), configFor)
	writeTestFile(t, filepath.Join(newDir, "api", "svc.tsp"), schemaFor("int64"))

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

// TestBuildTSEmitsFrontendExtras pins the opt-in ts-client MSW artifact and
// their presence in the drift manifest.
func TestBuildTSEmitsFrontendExtras(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), `module = "example.com/fe"

[generate.ts-client]
out = "web/client"
msw = true
`)
	writeTestFile(t, filepath.Join(dir, "svc.tsp"), `namespace fe;

model Req {
  id: string;
}

model Res {
  ok: boolean;
}

@route("/v1")
interface Svc {
  @get
  @route("/things/{id}")
  getThing(...Req): Res;
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, name := range []string{"types.ts", "client.ts", "msw.ts"} {
		data, err := os.ReadFile(filepath.Join(dir, "web", "client", name))
		if err != nil {
			t.Fatalf("expected generated %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, ".onekit", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest generationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	files := map[string]bool{}
	for _, listed := range manifest.Outputs["web/client"] {
		files[strings.TrimPrefix(listed, "web/client/")] = true
	}
	for _, name := range []string{"msw.ts"} {
		if !files[name] {
			t.Fatalf("manifest does not track %s: %+v", name, files)
		}
	}
}

func TestDiscoverSchemaFilesSkipsToolingDirsAndForeignSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model A {
  id: string;
}
`)
	writeTestFile(t, filepath.Join(dir, "node_modules", "pkg", "x.tsp"), "model Broken {\n")
	writeTestFile(t, filepath.Join(dir, ".git", "x.tsp"), "model Broken {\n")
	writeTestFile(t, filepath.Join(dir, "node_modules", "typescript", "bin", "tsc"), "#!/bin/sh\n")
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "node_modules", "typescript", "bin", "tsc"), filepath.Join(dir, "tools", "tsc")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	files, err := discoverProjectSchemaFiles(dir)
	if err != nil {
		t.Fatalf("discoverProjectSchemaFiles: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "api.tsp" {
		t.Fatalf("want only api.tsp, got %v", files)
	}
	if err := os.Symlink(filepath.Join(dir, "api.tsp"), filepath.Join(dir, "linked.tsp")); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverProjectSchemaFiles(dir); err == nil || !strings.Contains(err.Error(), "symlinked schema file") {
		t.Fatalf("want a symlinked schema file error, got %v", err)
	}
}
