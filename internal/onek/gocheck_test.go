package onek

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func buildGoSchema(t *testing.T, schema, testSource string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/check/api"

[generate.go-server]
out = "./api"

[generate.go-client]
out = "./api"
`)
	writeTestFile(t, filepath.Join(dir, "api.onk"), schema)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	apiDir := filepath.Join(dir, "api")
	writeTestFile(t, filepath.Join(apiDir, "go.mod"), "module example.com/check/api\n\ngo 1.26\n")
	args := []string{"vet", "./..."}
	if testSource != "" {
		writeTestFile(t, filepath.Join(apiDir, "schema_test.go"), testSource)
		args = []string{"test", "./..."}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = apiDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go package failed: %v\n%s", err, out)
	}
}

func TestGoFlattenTwoFieldsCompilesAndRoundTrips(t *testing.T) {
	buildGoSchema(t, `
package check

message Address {
  city: string
  zip: string
}

message Order {
  id: string
  billing: Address @flatten(prefix: "billing_")
  shipping: Address @flatten(prefix: "shipping_")
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestFlattenRoundTrip(t *testing.T) {
	in := Order{Id: "1", Billing: &Address{City: "a", Zip: "1"}, Shipping: &Address{City: "b", Zip: "2"}}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatal(err)
	}
	var out Order
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Billing == nil || out.Billing.City != "a" || out.Shipping == nil || out.Shipping.Zip != "2" {
		t.Fatalf("round trip lost data: %s -> %+v", data, out)
	}
}
`)
}

func TestGoBuildSanitizesHyphenatedPackageDirectories(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/hy/gen"

[generate.go-server]
out = "./gen"
`)
	writeTestFile(t, filepath.Join(dir, "shared-types", "models.onk"), "message Addr { city: string }\n")
	writeTestFile(t, filepath.Join(dir, "user-service", "api.onk"), `import "../shared-types/models.onk"
message U { id: string  addr: Addr }
service Users { get(U) -> U @get("/u/{id}") }
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	genDir := filepath.Join(dir, "gen")
	writeTestFile(t, filepath.Join(genDir, "go.mod"), "module example.com/hy/gen\n\ngo 1.26\n")
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = genDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go failed: %v\n%s", err, out)
	}
}
