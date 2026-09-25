package onek

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPythonPackagesImportFromANestedLocation(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"x\"\n\n[generate.python-client]\nout = \"./src/app/generated\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "models.onk"), "message Addr { city: string }\n")
	writeTestFile(t, filepath.Join(dir, "users", "api.onk"), `import "../common/models.onk"
message U { id: string  addr: Addr }
service Users { get(U) -> U @get("/u/{id}") }
`)
	writeTestFile(t, filepath.Join(dir, "src", "app", "__init__.py"), "")
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	cmd := exec.Command("python3", "-c", "import app.generated.users.client as c, app.generated.users.models as m; m.U(addr=None); print(c.UsersClient)")
	cmd.Dir = filepath.Join(dir, "src")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nested package import failed: %v\n%s", err, out)
	}
	flat := exec.Command("python3", "-c", "import users.client, users.models")
	flat.Dir = filepath.Join(dir, "src", "app", "generated")
	if out, err := flat.CombinedOutput(); err != nil {
		t.Fatalf("flat import from the output root failed: %v\n%s", err, out)
	}
}
