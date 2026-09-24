package onek

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPythonBuildWritesImportableHyphenatedPackages(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"x\"\n\n[generate.python-client]\nout = \"./py\"\n")
	writeTestFile(t, filepath.Join(dir, "shared-types", "models.onk"), "message Addr { city: string }\n")
	writeTestFile(t, filepath.Join(dir, "user-service", "api.onk"), `import "../shared-types/models.onk"
message U { id: string  addr: Addr }
service Users { get(U) -> U @get("/u/{id}") }
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	cmd := exec.Command("python3", "-c", "import user_service.client, user_service.models, shared_types.models")
	cmd.Dir = filepath.Join(dir, "py")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Python is not importable: %v\n%s", err, out)
	}
}
