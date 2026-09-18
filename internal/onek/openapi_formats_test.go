package onek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/pb33f/libopenapi"
	"sigs.k8s.io/yaml"
)

func TestBuildOpenAPIBothFormats(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), onekitToml)
	writeTestFile(t, filepath.Join(dir, "models.onk"), modelsOnk)
	writeTestFile(t, filepath.Join(dir, "service.onk"), serviceOnk)
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	var documents []any
	for _, name := range []string{"openapi.yaml", "openapi.json"} {
		data, err := os.ReadFile(filepath.Join(dir, "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := libopenapi.NewDocument(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := doc.BuildV3Model(); err != nil {
			t.Fatal(err)
		}
		if name == "openapi.yaml" {
			data, err = yaml.YAMLToJSON(data)
			if err != nil {
				t.Fatal(err)
			}
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatal(err)
		}
		documents = append(documents, parsed)
	}
	if !reflect.DeepEqual(documents[0], documents[1]) {
		t.Fatal("YAML and JSON describe different APIs")
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, ".onekit", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest generationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docs/openapi.yaml", "docs/openapi.json"} {
		if !slices.Contains(manifest.Outputs["docs"], name) {
			t.Fatalf("manifest missing %s", name)
		}
	}
	// Rebuilding a YAML-only output directory must restore the JSON document.
	jsonPath := filepath.Join(dir, "docs", "openapi.json")
	if err := os.Remove(jsonPath); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("missing JSON was not regenerated: %v", err)
	}
}
