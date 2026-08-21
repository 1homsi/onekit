package onek

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFormatAndGenerationManifest(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
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
	if err := Format(dir, true); err != nil {
		t.Fatalf("formatted init project failed check: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "api.onk"), []byte("message X{}\n"), 0o644); err != nil {
		t.Fatalf("write unformatted schema: %v", err)
	}
	var formatErr *FormatError
	if err := Format(dir, true); !errors.As(err, &formatErr) || len(formatErr.Files) != 1 {
		t.Fatalf("expected one formatting finding, got %v", err)
	}
	if err := Format(dir, false); err != nil {
		t.Fatalf("format project: %v", err)
	}
	if err := Format(dir, true); err != nil {
		t.Fatalf("formatted project failed second check: %v", err)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	partial := t.TempDir()
	if err := os.WriteFile(filepath.Join(partial, "api.onk"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(partial, false); err == nil {
		t.Fatal("expected init to refuse a partially existing project")
	}
	if _, err := os.Stat(filepath.Join(partial, "onekit.toml")); !os.IsNotExist(err) {
		t.Fatalf("init refusal should not create onekit.toml, stat error: %v", err)
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
	path := filepath.Join(t.TempDir(), "broken.onk")
	if err := os.WriteFile(path, []byte("message Broken {\n  value: string\n"), 0o644); err != nil {
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

// TestFormatNeverDeletesSchemaFiles pins the fix for formatting deleting
// source files: writeFile's remove-on-empty contract exists for stale
// generated outputs, and a comment-only .onk file must survive onek fmt with
// its content intact.
func TestFormatNeverDeletesSchemaFiles(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.onk")
	api := filepath.Join(dir, "api.onk")
	if err := os.WriteFile(notes, []byte("// just some notes\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.WriteFile(api, []byte("package app\nmessage R { x: string }\n// license note\n"), 0o644); err != nil {
		t.Fatalf("write api: %v", err)
	}
	if err := Format(dir, false); err != nil {
		t.Fatalf("format: %v", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Fatalf("comment-only schema file was deleted by fmt: %v", err)
	}
	data, err := os.ReadFile(api)
	if err != nil {
		t.Fatalf("read api: %v", err)
	}
	if !strings.Contains(string(data), "// license note") {
		t.Fatalf("trailing comment was stripped by fmt:\n%s", data)
	}
	formattedNotes, err := os.ReadFile(notes)
	if err != nil {
		t.Fatalf("read notes: %v", err)
	}
	if strings.TrimSpace(string(formattedNotes)) != "// just some notes" {
		t.Fatalf("notes content changed:\n%s", formattedNotes)
	}
}
