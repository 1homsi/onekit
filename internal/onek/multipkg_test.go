package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const multiPkgOnekitToml = `
module = "example.com/voxie/gen/go"

[generate.ts-client]
out = "./gen/ts"

[generate.python-client]
out = "./gen/py"
`

func TestBuildResolvesCrossPackageTypesForTSAndPython(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), multiPkgOnekitToml)
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)

	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	tsTypes, err := os.ReadFile(filepath.Join(dir, "gen", "ts", "hub", "business", "v1", "types.ts"))
	if err != nil {
		t.Fatalf("read business types.ts: %v", err)
	}
	if !containsString(string(tsTypes), `import * as common from "../../../common/types";`) {
		t.Fatalf("expected business types.ts to import the common package, got:\n%s", tsTypes)
	}
	if !containsString(string(tsTypes), "common.Money") {
		t.Fatalf("expected business types.ts to reference common.Money, got:\n%s", tsTypes)
	}

	pyTypes, err := os.ReadFile(filepath.Join(dir, "gen", "py", "hub", "business", "v1", "models.py"))
	if err != nil {
		t.Fatalf("read business models.py: %v", err)
	}
	if !containsString(string(pyTypes), "import common.models as common_models") {
		t.Fatalf("expected business models.py to import the common package, got:\n%s", pyTypes)
	}
	if !containsString(string(pyTypes), "common_models.Money") {
		t.Fatalf("expected business models.py to reference common_models.Money, got:\n%s", pyTypes)
	}

	for _, rel := range []string{
		filepath.Join("gen", "py", "__init__.py"),
		filepath.Join("gen", "py", "common", "__init__.py"),
		filepath.Join("gen", "py", "hub", "__init__.py"),
		filepath.Join("gen", "py", "hub", "business", "__init__.py"),
		filepath.Join("gen", "py", "hub", "business", "v1", "__init__.py"),
	} {
		if _, statErr := os.Stat(filepath.Join(dir, rel)); statErr != nil {
			t.Fatalf("expected __init__.py at %s: %v", rel, statErr)
		}
	}

	if _, lookErr := exec.LookPath("python3"); lookErr == nil {
		cmd := exec.Command("python3", "-c", "from hub.business.v1.models import Business; "+
			"b = Business(id='1', name='Acme', balance={'amount_cents': 100}); print('OK')")
		cmd.Dir = filepath.Join(dir, "gen", "py")
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("python cross-package import failed: %v\n%s", runErr, out)
		}
	}
}
