package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const runtimeOnk = `
package jsruntime

message Ping { id: string }

service Pings {
  base_path: "/pings"
  ping(Ping) -> Ping @post("/ping")
}
`

func writePackageProject(t *testing.T, targets string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/voxie/gen\"\n\n"+targets)
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "jsruntime", "ping.onk"), runtimeOnk)
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestPackageFiltersChoosePackagesPerTarget(t *testing.T) {
	targets := `
[generate.ts-client]
out = "gen/web"
exclude_packages = ["jsruntime"]

[generate.ts-server]
out = "gen/runtime"
include_packages = ["jsruntime"]

[generate.go-server]
out = "gen/go"
exclude_packages = ["jsruntime"]
`
	dir := writePackageProject(t, targets)
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"gen/web/common/types.ts":              true,
		"gen/web/hub/business/v1/client.ts":    true,
		"gen/web/jsruntime/types.ts":           false,
		"gen/runtime/jsruntime/server.ts":      true,
		"gen/runtime/common/types.ts":          false,
		"gen/runtime/hub/business/v1/types.ts": false,
		"gen/go/common/types.gen.go":           true,
		"gen/go/jsruntime/types.gen.go":        false,
		"gen/go/hub/business/v1/server.gen.go": true,
	} {
		if got := exists(filepath.Join(dir, filepath.FromSlash(path))); got != want {
			t.Errorf("%s exists = %v, want %v", path, got, want)
		}
	}
}

func TestPackageFilterRemovesOutputsItNoLongerKeeps(t *testing.T) {
	dir := writePackageProject(t, "[generate.ts-client]\nout = \"gen/web\"\n")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "gen", "web", "jsruntime", "types.ts")
	if !exists(stale) {
		t.Fatal("the unfiltered build should write jsruntime")
	}
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/voxie/gen\"\n\n[generate.ts-client]\nout = \"gen/web\"\nexclude_packages = [\"jsruntime\"]\n")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	if exists(stale) {
		t.Fatal("a package the filter drops must not be left behind")
	}
}

func TestPackageFilterRejectsDanglingPackageReferences(t *testing.T) {
	dir := writePackageProject(t, "[generate.ts-client]\nout = \"gen/web\"\nexclude_packages = [\"common\"]\n")
	err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), "leaves out") || !strings.Contains(err.Error(), "common") {
		t.Fatalf("expected a dangling-reference error naming the package, got %v", err)
	}
}

func TestPackageFilterRejectsPatternsThatMatchNothing(t *testing.T) {
	dir := writePackageProject(t, "[generate.ts-client]\nout = \"gen/web\"\ninclude_packages = [\"nope\"]\n")
	err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), "matches no package") {
		t.Fatalf("expected a no-match error, got %v", err)
	}
}
