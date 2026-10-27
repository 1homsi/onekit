package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateExample migrates the .onk schema examples/simple-api was
// written in and requires the result to build to the example's checked-in
// output.
func TestMigrateExample(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "examples", "simple-api"), dir)
	for _, name := range []string{"main.tsp", "models.tsp", "package.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(t, filepath.Join("testdata", "simple-api-onk"), dir)
	// The example predates onkt.toml in this test's story.
	if err := os.Rename(filepath.Join(dir, configFileName), filepath.Join(dir, legacyConfigFileName)); err != nil {
		t.Fatal(err)
	}

	result, err := Migrate(dir, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.tsp", "service.tsp", "main.tsp", "package.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("migrate did not write %s: %v", name, err)
		}
	}
	for _, name := range []string{"models.onk", "service.onk"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("migrate left %s in place", name)
		}
	}
	if len(result.Removed) != 3 {
		t.Errorf("removed = %v, want the two .onk files and onekit.toml", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "onkt.toml")); err != nil {
		t.Errorf("migrate did not rename onekit.toml to onkt.toml: %v", err)
	}

	linkTypeSpec(t, dir)
	t.Setenv("ONEK_NO_CACHE", "1")
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	requireExampleOutputs(t, dir)
}

func TestMigrateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	writeTestFile(t, filepath.Join(dir, "api.tsp"), "// mine\n")
	_, err := Migrate(dir, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal naming --force", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("a refused migration must not remove the .onk file")
	}
}

func TestMigrateKeepsSchemasOnRequest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	if _, err := Migrate(dir, MigrateOptions{KeepSchemas: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.onk")); err != nil {
		t.Fatal("--keep must leave the .onk file in place")
	}
	main, err := os.ReadFile(filepath.Join(dir, "main.tsp"))
	if err != nil || !strings.Contains(string(main), `import "./api.tsp";`) {
		t.Fatalf("main.tsp = %q, %v", main, err)
	}
}

func TestBuildPointsOnkProjectsAtMigrate(t *testing.T) {
	onkSchemasAccepted = false
	t.Cleanup(func() { onkSchemasAccepted = true })
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "onkt migrate") {
		t.Fatalf("err = %v, want a pointer to onkt migrate", err)
	}
}

func TestPackageJSONPinsTheMatchingEmitter(t *testing.T) {
	previous := ToolVersion
	t.Cleanup(func() { ToolVersion = previous })
	for version, want := range map[string]string{"v0.37.0": `"@onkt/typespec": "^0.37.0"`, "dev": `"@onkt/typespec": "latest"`, "(devel)": `"@onkt/typespec": "latest"`} {
		ToolVersion = version
		if got := typespecPackageJSON(); !strings.Contains(got, want) {
			t.Errorf("%s: package.json = %s, want %s", version, got, want)
		}
	}
}
