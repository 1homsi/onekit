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

func TestOpenAPISplitsServicesAndCleansCombinedOutputs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `module = "example.com/test"
schema_root = "api"
[generate.openapi]
out = "gen/openapi"
`)
	writeTestFile(t, filepath.Join(dir, "api/common/money.onk"), commonMoneyOnk+"\nmessage Unused { value: string }\n")
	writeTestFile(t, filepath.Join(dir, "api/hub/business/v1/service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "api/hr/time_entry/v1/service.onk"), timeEntryServiceOnk)
	root := filepath.Join(dir, "gen/openapi")
	// Simulate the manifest from the previous combined generator.
	for _, name := range []string{"openapi.yaml", "openapi.json", "notes.txt"} {
		writeTestFile(t, filepath.Join(root, name), "old")
	}
	writeTestFile(t, filepath.Join(dir, ".onekit/manifest.json"), `{"outputs":{"gen/openapi":["gen/openapi/openapi.yaml","gen/openapi/openapi.json"]}}`)
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openapi.yaml", "openapi.json", "common/openapi.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected output %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal("removed unrelated file", err)
	}
	for _, tc := range []struct {
		dir, route string
		schemas    int
	}{
		{"hub/business/v1", "/hub/business/v1/businesses/{id}", 3},
		{"hr/time_entry/v1", "/hr/time-entry/v1/entries", 2},
	} {
		var values []any
		for _, ext := range []string{"yaml", "json"} {
			data, err := os.ReadFile(filepath.Join(root, tc.dir, "openapi."+ext))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := libopenapi.NewDocument(data)
			if err != nil {
				t.Fatal(err)
			}
			model, err := doc.BuildV3Model()
			if err != nil {
				t.Fatal(err)
			}
			if model.Model.Paths.PathItems.Len() != 1 {
				t.Fatalf("%s includes another service", tc.dir)
			}
			if _, ok := model.Model.Paths.PathItems.Get(tc.route); !ok {
				t.Fatalf("missing route %s", tc.route)
			}
			if model.Model.Components.Schemas.Len() != tc.schemas {
				t.Fatalf("%s unexpected dependency schemas: %d", tc.dir, model.Model.Components.Schemas.Len())
			}
			if ext == "yaml" {
				data, err = yaml.YAMLToJSON(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		if !reflect.DeepEqual(values[0], values[1]) {
			t.Fatal("formats differ", tc.dir)
		}
	}
	// Multiple services in one directory must not overwrite each other.
	extra := filepath.Join(dir, "api/hub/business/v1/extra.onk")
	writeTestFile(t, extra, `package hub.business
service ExtraService { fetch(GetBusinessRequest) -> Business @get("/extra/{id}") }
`)
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"BusinessService", "ExtraService"} {
		for _, ext := range []string{"yaml", "json"} {
			if _, err := os.Stat(filepath.Join(root, "hub/business/v1", service+".openapi."+ext)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "hub/business/v1/openapi.yaml")); !os.IsNotExist(err) {
		t.Fatal("stale single-service document retained", err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "hub/business/v1/ExtraService.openapi.json")); !os.IsNotExist(err) {
		t.Fatal("deleted service document retained", err)
	}
}
