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
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios/Sources/Api\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "money.tsp"), commonMoneyTypeSpec)
	writeTestFile(t, filepath.Join(dir, "common", "shared.tsp"), strictESMSharedTypeSpec)
	writeTestFile(t, filepath.Join(dir, "hub", "catalog", "v1", "service.tsp"), strictESMCatalogTypeSpec)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.tsp"), businessServiceTypeSpec)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.tsp"), strictESMRuntimeTypeSpec)
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

func swiftTypeCheck(t *testing.T, root string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		return
	}
	var files []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".swift" {
			files = append(files, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("swiftc", append([]string{"-typecheck", "-warnings-as-errors"}, files...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("swiftc -typecheck: %v\n%s", err, output)
	}
}

func TestBuildSwiftClientNamespacesPackagesThatShareTypeNames(t *testing.T) {
	dir := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios\"\n")
	writeTestFile(t, filepath.Join(dir, "a", "a.tsp"), `namespace a;

model Item {
  x: string;
}

model Req {
  id: string;
}

interface A {
  @get
  @route("/a/{id}")
  get(...Req): Item;
}
`)
	writeTestFile(t, filepath.Join(dir, "b", "v1", "b.tsp"), `namespace b;

model Item {
  y: int32;
}

model Req {
  id: string;
}

model Uses {
  a: Item;
}

interface B {
  @get
  @route("/b/{id}")
  get(...Req): Uses;
}
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	a, err := os.ReadFile(filepath.Join(dir, "ios", "a", "a__Models.swift"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "ios", "b", "v1", "b__v1__Models.swift"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"public enum A {}", "extension A {", "public final class Item:"} {
		if !strings.Contains(string(a), want) {
			t.Fatalf("package a is missing %q:\n%s", want, a)
		}
	}
	for _, want := range []string{"public enum BV1 {}", "extension BV1 {", "public final class Item:"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("package b is missing %q:\n%s", want, b)
		}
	}
	swiftTypeCheck(t, filepath.Join(dir, "ios"))
}

func TestBuildSwiftClientQualifiesReferencesAcrossNamespaces(t *testing.T) {
	dir := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "money.tsp"), commonMoneyTypeSpec)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.tsp"), businessServiceTypeSpec)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	models, err := os.ReadFile(filepath.Join(dir, "ios", "hub", "business", "v1", "hub__business__v1__Models.swift"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(models), "extension HubBusinessV1 {") || !strings.Contains(string(models), "Common.Money") {
		t.Fatalf("expected a namespace and a qualified reference to Common.Money:\n%s", models)
	}
	swiftTypeCheck(t, filepath.Join(dir, "ios"))
}

func TestBuildSwiftClientRejectsGenuineNameCollisions(t *testing.T) {
	dir := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios\"\n")
	writeTestFile(t, filepath.Join(dir, "common", "a.tsp"), `namespace common;

model Item {
  x: string;
}

model Req {
  id: string;
}

interface C {
  @get
  @route("/c/{id}")
  get(...Req): Item;
}
`)
	writeTestFile(t, filepath.Join(dir, "root.tsp"), `namespace root;

model Common {
  y: int32;
}
`)
	err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), `"Common"`) || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("a root type named like a package namespace must be rejected clearly, got %v", err)
	}

	dup := swiftTempDir(t)
	writeTestFile(t, filepath.Join(dup, "onkt.toml"), "module = \"example.com/app\"\n\n[generate.swift-client]\nout = \"./ios\"\n")
	writeTestFile(t, filepath.Join(dup, "a-b", "x.tsp"), `namespace p;

model M {
  x: string;
}
`)
	writeTestFile(t, filepath.Join(dup, "a", "b", "y.tsp"), `namespace q;

model N {
  x: string;
}
`)
	err = Build(dup)
	if err == nil || !strings.Contains(err.Error(), "both map to the Swift namespace") {
		t.Fatalf("two directories with one namespace must be rejected, got %v", err)
	}
}
