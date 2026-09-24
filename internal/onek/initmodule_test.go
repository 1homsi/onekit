package onek

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInitModulePathMatchesGeneratedPackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := filepath.Join(t.TempDir(), "Billing API")
	if err := Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/billing-api\n\ngo 1.26\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"net/http"

	api "example.com/billing-api/gen"
)

func main() {
	_ = api.NewHealthServiceClient("http://localhost")
	_ = http.NewServeMux()
}
`)
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("project created by init does not build: %v\n%s", err, out)
	}
}
