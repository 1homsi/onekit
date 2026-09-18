package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const commonMoneyOnk = `
package common

message Money {
  amount_cents: int64
  currency: string
}
`

const businessServiceOnk = `
package hub.business

message GetBusinessRequest {
  id: string
}

message Business {
  id: string
  name: string
  balance: Money
}

service BusinessService {
  getBusiness(GetBusinessRequest) -> Business @get("/businesses/{id}")
}
`

const timeEntryServiceOnk = `
package hr.time_entry

message ListEntriesRequest {
  employee_id: string @query("employee_id")
}

message Entry {
  id: string
}

service TimeEntryService {
  listEntries(ListEntriesRequest) -> Entry @get("/entries")
}
`

const multiServiceOnekitToml = `
module = "example.com/voxie/gen/go"
route_prefix = "/api"

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

[generate.openapi]
out = "./gen/openapi"
`

func TestBuildInfersBasePathAndSplitsPackagesByDirectory(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), multiServiceOnekitToml)
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "hr", "time_entry", "v1", "service.onk"), timeEntryServiceOnk)

	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	genRoot := filepath.Join(dir, "gen", "go")
	for _, rel := range []string{
		filepath.Join("common", "types.gen.go"),
		filepath.Join("hub", "business", "v1", "types.gen.go"),
		filepath.Join("hub", "business", "v1", "server.gen.go"),
		filepath.Join("hub", "business", "v1", "client.gen.go"),
		filepath.Join("hr", "time_entry", "v1", "types.gen.go"),
		filepath.Join("hr", "time_entry", "v1", "server.gen.go"),
	} {
		if _, err := os.Stat(filepath.Join(genRoot, rel)); err != nil {
			t.Fatalf("expected generated file %s: %v", rel, err)
		}
	}

	businessServer, err := os.ReadFile(filepath.Join(genRoot, "hub", "business", "v1", "server.gen.go"))
	if err != nil {
		t.Fatalf("read business server.gen.go: %v", err)
	}
	if !containsString(string(businessServer), `/api/hub/business/v1/businesses/{id}`) {
		t.Fatalf("expected prefixed base_path /api/hub/business/v1 in business server, got:\n%s", businessServer)
	}

	timeEntryServer, err := os.ReadFile(filepath.Join(genRoot, "hr", "time_entry", "v1", "server.gen.go"))
	if err != nil {
		t.Fatalf("read time_entry server.gen.go: %v", err)
	}
	if !containsString(string(timeEntryServer), `/api/hr/time-entry/v1/entries`) {
		t.Fatalf("expected prefixed, kebab-cased base_path /api/hr/time-entry/v1 in time_entry server, got:\n%s",
			timeEntryServer)
	}

	route := `/api/hub/business/v1/businesses/{id}`
	generatedRoutes := []struct {
		name string
		path string
	}{
		{"Go client", filepath.Join(genRoot, "hub", "business", "v1", "client.gen.go")},
		{"TypeScript client", filepath.Join(dir, "gen", "ts", "hub", "business", "v1", "client.ts")},
		{"TypeScript server", filepath.Join(dir, "gen", "ts-server", "hub", "business", "v1", "server.ts")},
		{"Python client", filepath.Join(dir, "gen", "python", "hub", "business", "v1", "client.py")},
		{"OpenAPI", filepath.Join(dir, "gen", "openapi", "hub", "business", "v1", "openapi.yaml")},
	}
	for _, generated := range generatedRoutes {
		data, readErr := os.ReadFile(generated.path)
		if readErr != nil {
			t.Fatalf("read %s output: %v", generated.name, readErr)
		}
		if !containsString(string(data), route) {
			t.Fatalf("expected route prefix in %s output, got:\n%s", generated.name, data)
		}
	}

	businessTypes, err := os.ReadFile(filepath.Join(genRoot, "hub", "business", "v1", "types.gen.go"))
	if err != nil {
		t.Fatalf("read business types.gen.go: %v", err)
	}
	if !containsString(string(businessTypes), `"example.com/voxie/gen/go/common"`) {
		t.Fatalf("expected business types.gen.go to import the common package, got:\n%s", businessTypes)
	}
	if !containsString(string(businessTypes), "common.Money") {
		t.Fatalf("expected business types.gen.go to reference common.Money, got:\n%s", businessTypes)
	}

	writeTestFile(t, filepath.Join(genRoot, "go.mod"), "module example.com/voxie/gen/go\n\ngo 1.26\n")
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = genRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated multi-package tree failed to build: %v\n%s", err, out)
	}
}

func containsString(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}())
}
