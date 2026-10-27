package onek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitGenerationManifest(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	linkTypeSpec(t, dir)
	if err := Build(dir); err != nil {
		t.Fatalf("Build after Init: %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, ".onekit", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest generationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.SchemaHash == "" || len(manifest.SchemaFiles) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	partial := t.TempDir()
	if err := os.WriteFile(filepath.Join(partial, "main.tsp"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(partial, false); err == nil {
		t.Fatal("expected init to refuse a partially existing project")
	}
	if _, err := os.Stat(filepath.Join(partial, "onkt.toml")); !os.IsNotExist(err) {
		t.Fatalf("init refusal should not create onkt.toml, stat error: %v", err)
	}

	dir := t.TempDir()
	if err := Init(dir, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := Init(dir, false); err == nil {
		t.Fatal("expected second Init to refuse overwrite")
	}
	if err := Init(dir, true); err != nil {
		t.Fatalf("forced Init: %v", err)
	}
}

func TestDiagnosticsPreserveParserLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.tsp")
	if err := os.WriteFile(path, []byte("model Broken {\n  value: string\n"), 0o644); err != nil {
		t.Fatalf("write broken schema: %v", err)
	}
	_, err := parseSources([]string{path})
	if err == nil {
		t.Fatal("expected parser error")
	}
	diagnostics := Diagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Path != path || diagnostics[0].Line == 0 || diagnostics[0].Column == 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}
