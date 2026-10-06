package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const filterSchema = `package app

message Item { id: string }

service Public {
  base_path: "/v1"
  list(Item) -> Item @get("/items")
}

service Admin {
  base_path: "/admin"
  purge(Item) -> Item @post("/purge")
}
`

func buildFiltered(t *testing.T, targets string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app/gen\"\n\n"+targets)
	writeTestFile(t, filepath.Join(dir, "api.onk"), filterSchema)
	return dir, Build(dir)
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestServiceFiltersChooseServicesPerTarget(t *testing.T) {
	dir, err := buildFiltered(t, `
[generate.go-server]
out = "gen/go"
include_services = ["Public"]

[generate.go-client]
out = "gen/go"

[generate.ts-client]
out = "gen/ts"
exclude_services = ["Admin*"]

[generate.openapi]
out = "gen/openapi"
include_services = ["Admin"]

[generate.rust-server]
out = "gen/rust"
exclude_services = ["Admin"]
`)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	goServer := read(t, filepath.Join(dir, "gen", "go", "server.gen.go"))
	if !strings.Contains(goServer, "RegisterPublicServer") || strings.Contains(goServer, "RegisterAdminServer") {
		t.Errorf("go-server should only serve Public")
	}
	goClient := read(t, filepath.Join(dir, "gen", "go", "client.gen.go"))
	if !strings.Contains(goClient, "AdminClient") || !strings.Contains(goClient, "PublicClient") {
		t.Errorf("go-client has no filter and should keep both services")
	}
	tsClient := read(t, filepath.Join(dir, "gen", "ts", "client.ts"))
	if !strings.Contains(tsClient, "PublicClient") || strings.Contains(tsClient, "AdminClient") {
		t.Errorf("ts-client should exclude Admin")
	}
	types := read(t, filepath.Join(dir, "gen", "ts", "types.ts"))
	if !strings.Contains(types, "interface Item") {
		t.Errorf("types are always generated in full")
	}
	api := read(t, filepath.Join(dir, "gen", "openapi", "openapi.yaml"))
	if !strings.Contains(api, "/admin/purge") || strings.Contains(api, "/v1/items") {
		t.Errorf("openapi should document only Admin:\n%s", api)
	}
	rust := read(t, filepath.Join(dir, "gen", "rust", "server.rs"))
	if !strings.Contains(rust, "pub trait Public") || strings.Contains(rust, "pub trait Admin") {
		t.Errorf("rust-server should exclude Admin")
	}
}

func TestServiceFilterRemovesTheOutputWhenNoServiceRemains(t *testing.T) {
	dir, err := buildFiltered(t, "[generate.dart-client]\nout = \"gen/dart\"\n")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	client := filepath.Join(dir, "gen", "dart", "client.dart")
	if _, err := os.Stat(client); err != nil {
		t.Fatalf("expected a Dart client without a filter: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app/gen\"\n\n[generate.dart-client]\nout = \"gen/dart\"\nexclude_services = [\"*\"]\n")
	if err := Build(dir); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if _, err := os.Stat(client); !os.IsNotExist(err) {
		t.Fatalf("client.dart should be removed once every service is filtered out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "dart", "models.dart")); err != nil {
		t.Fatalf("models are still generated: %v", err)
	}
}

func TestServiceFilterRejectsPatternsThatMatchNothingOrAreInvalid(t *testing.T) {
	for name, tc := range map[string]struct{ filter, want string }{
		"typo":         {`include_services = ["Publik"]`, `include_services pattern "Publik" matches no service`},
		"exclude typo": {`exclude_services = ["Nope*"]`, `exclude_services pattern "Nope*" matches no service`},
		"bad pattern":  {`include_services = ["[x"]`, "invalid pattern"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildFiltered(t, "[generate.go-server]\nout = \"gen\"\n"+tc.filter+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
