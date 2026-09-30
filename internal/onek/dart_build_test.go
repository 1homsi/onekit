package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDartClientAnalyzesCleanAcrossPackages(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app\"\n\n[generate.dart-client]\nout = \"./app/lib/api\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "common", "shared.onk"), strictESMSharedOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "catalog", "v1", "service.onk"), strictESMCatalogOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.onk"), strictESMRuntimeOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	out := filepath.Join(dir, "app", "lib", "api")
	for _, name := range []string{
		"onekit.dart", "onekit_ws.dart", "onekit_ws_io.dart", "onekit_ws_web.dart",
		"common/models.dart", "hub/catalog/v1/models.dart", "hub/catalog/v1/client.dart",
		"hub/business/v1/client.dart", "rt/client.dart",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "common", "client.dart")); !os.IsNotExist(err) {
		t.Fatalf("a package without services must not get a client: %v", err)
	}
	app := filepath.Join(dir, "app")
	writeTestFile(t, filepath.Join(app, "pubspec.yaml"), "name: app\nenvironment:\n  sdk: ^3.4.0\ndependencies:\n  http: ^1.2.0\n  web_socket_channel: ^3.0.0\n")
	runIn(t, app, "dart", "pub", "get")
	cmd := exec.Command("dart", "analyze", "--fatal-infos", "lib")
	cmd.Dir = app
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "No issues found") {
		t.Fatalf("dart analyze: %v\n%s", err, output)
	}
}
