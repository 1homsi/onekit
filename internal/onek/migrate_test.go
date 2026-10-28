package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The .onk schema examples/simple-api was written in before TypeSpec.
const (
	simpleAPIModelsOnk = `package examples.onksimpleapi.models

/// A system user.
message User {
  id: string @uuid

  name: string @len(1, 100)

  email: string @email

  created_at: int64
}

message CreateUserRequest {
  name: string @len(2, 100)

  email: string @email
}

message GetUserRequest {
  id: string @uuid
}

message EmailAuth {
  email: string @email

  password: string @len(8, 72)

  use_two_factor: bool

  two_factor_code: string
}

message TokenAuth {
  token: string @len(10, 256)

  token_type: string

  issued_at: int64
}

message SocialAuth {
  provider: string @in("google", "github", "facebook", "apple")

  access_token: string @len(20, 512)

  redirect_uri: string

  state: string

  permissions: string[]
}

message LoginRequest {
  device_id: string

  ip_address: string

  retry_count: int32

  user_agent: string

  auth_method: oneof(discriminator: "auth_type") {
    email: EmailAuth

    token: TokenAuth

    social: SocialAuth
  }

  remember_me: bool

  session_id: string

  timestamp: int64

  scopes: string[]
}

message LoginResponse {
  access_token: string

  refresh_token: string

  expires_in: int64

  user: User
}
`
	simpleAPIServiceOnk = `package examples.onksimpleapi.services

/// UserService manages users in the system.
service UserService {
  base_path: "/api/v1"
  headers: {
    "X-API-Key": string @required @format("uuid")
  }

  /// Create a new user.
  createUser(CreateUserRequest) -> User @post("/users")

  /// Get a user by ID.
  getUser(GetUserRequest) -> User @post("/users/get")

  /// Login with different authentication methods.
  login(LoginRequest) -> LoginResponse @post("/auth/login") {
    headers: {
      "X-Request-Id": string @required @format("uuid")
    }
  }
}
`
)

// TestMigrateExample migrates the .onk schema examples/simple-api was
// written in and requires the result to build to the example's checked-in
// output.
func TestMigrateExample(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "examples", "simple-api"), dir)
	for _, name := range []string{"main.tsp", "models.tsp", "package.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(dir, "models.onk"), simpleAPIModelsOnk)
	writeTestFile(t, filepath.Join(dir, "service.onk"), simpleAPIServiceOnk)
	// The example predates onkt.toml in this test's story.
	if err := os.Rename(filepath.Join(dir, configFileName), filepath.Join(dir, legacyConfigFileName)); err != nil {
		t.Fatal(err)
	}

	result, err := Migrate(dir, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.tsp", "service.tsp", "main.tsp", "package.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("migrate did not write %s: %v", name, err)
		}
	}
	for _, name := range []string{"models.onk", "service.onk"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("migrate left %s in place", name)
		}
	}
	if len(result.Removed) != 3 {
		t.Errorf("removed = %v, want the two .onk files and onekit.toml", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "onkt.toml")); err != nil {
		t.Errorf("migrate did not rename onekit.toml to onkt.toml: %v", err)
	}

	linkTypeSpec(t, dir)
	t.Setenv("ONEK_NO_CACHE", "1")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	requireExampleOutputs(t, dir)
}

func TestMigrateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	writeTestFile(t, filepath.Join(dir, "api.tsp"), "// mine\n")
	_, err := Migrate(dir, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal naming --force", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("a refused migration must not remove the .onk file")
	}
}

func TestMigrateKeepsSchemasOnRequest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	if _, err := Migrate(dir, MigrateOptions{KeepSchemas: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("--keep must leave the .onk file in place")
	}
	main, err := os.ReadFile(filepath.Join(dir, "main.tsp"))
	if err != nil || !strings.Contains(string(main), `import "./api.tsp";`) {
		t.Fatalf("main.tsp = %q, %v", main, err)
	}
}

func TestBuildPointsOnkProjectsAtMigrate(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "onkt migrate") {
		t.Fatalf("err = %v, want a pointer to onkt migrate", err)
	}
}

func TestPackageJSONPinsTheMatchingEmitterAndCLI(t *testing.T) {
	previous := toolVersion
	t.Cleanup(func() { SetToolVersion(previous) })
	for version, want := range map[string]string{"v0.37.0": `"@onkt/typespec": "^0.37.0"`, "dev": `"@onkt/typespec": "latest"`, "(devel)": `"@onkt/typespec": "latest"`} {
		cli := strings.Replace(want, "@onkt/typespec", "@onkt/cli", 1)
		SetToolVersion(version)
		if got := typespecPackageJSON(); !strings.Contains(got, want) || !strings.Contains(got, cli) {
			t.Errorf("%s: package.json = %s, want %s and %s", version, got, want, cli)
		}
	}
}

// A pin to a release from before TypeSpec would hand every build after the
// migration to a release that cannot read it, so the pin moves along.
func TestMigrateMovesAnOldPinToTheRunningRelease(t *testing.T) {
	defer SetToolVersion(toolVersion)
	SetToolVersion("0.38.0")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api", "models.onk"), simpleAPIModelsOnk)
	writeTestFile(t, filepath.Join(dir, legacyConfigFileName), "version = \"0.36.3\"\nmodule = \"example.com/x\"\n\n[generate.openapi]\nout = \"docs\"\nversion = \"1.0.0\"\n")
	writeTestFile(t, filepath.Join(dir, "package.json"), `{"private": true}`)
	result, err := Migrate(dir, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	if want := "version = \"0.38.0\"\nmodule = \"example.com/x\"\n\n[generate.openapi]\nout = \"docs\"\nversion = \"1.0.0\"\n"; string(config) != want {
		t.Fatalf("onkt.toml =\n%s\nwant\n%s", config, want)
	}
	if len(result.Notes) != 2 || !strings.Contains(result.Notes[0], "@onkt/typespec") || !strings.Contains(result.Notes[1], `"0.36.3" to "0.38.0"`) {
		t.Fatalf("notes = %q", result.Notes)
	}
}
