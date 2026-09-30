package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func swiftTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildSwiftClientTypeChecksAcrossPackages(t *testing.T) {
	dir := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios/Sources/Api\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "common", "shared.onk"), strictESMSharedOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "catalog", "v1", "service.onk"), strictESMCatalogOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.onk"), strictESMRuntimeOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	out := filepath.Join(dir, "ios", "Sources", "Api")
	for _, name := range []string{
		"Onekit.swift", "common/common__Models.swift", "hub/catalog/v1/hub__catalog__v1__Models.swift",
		"hub/catalog/v1/hub__catalog__v1__Client.swift", "hub/business/v1/hub__business__v1__Client.swift",
		"rt/rt__Models.swift", "rt/rt__Client.swift",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "common", "common__Client.swift")); !os.IsNotExist(err) {
		t.Fatalf("a package without services must not get a client: %v", err)
	}
	if err := VerifyGenerated(dir); err != nil {
		t.Fatalf("VerifyGenerated: %v", err)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	var files []string
	if err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".swift" {
			files = append(files, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-typecheck", "-warnings-as-errors"}, files...)
	cmd := exec.Command("swiftc", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("swiftc -typecheck: %v\n%s", err, output)
	}
}

func TestBuildSwiftClientRejectsTypeNamesSharedBetweenPackages(t *testing.T) {
	dir := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios\"\n")
	writeTestFile(t, filepath.Join(dir, "a", "a.onk"), "package a\nmessage Item { x: string }\nmessage Req { id: string }\nservice A { get(Req) -> Item @get(\"/a/{id}\") }\n")
	writeTestFile(t, filepath.Join(dir, "b", "b.onk"), "package b\nmessage Item { y: int32 }\nmessage Req2 { id: string }\nservice B { get(Req2) -> Item @get(\"/b/{id}\") }\n")
	err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), `"Item" is declared in both a and b`) {
		t.Fatalf("expected a clear duplicate type error, got %v", err)
	}
}
