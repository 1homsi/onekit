package onek

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const everyTargetCatalogOnk = `
package hub.catalog

service CatalogService {
  base_path: "/catalog/v1"

  getPrice(Lookup) -> Money | NotFound @get("/prices/{id}")
  setPrice(Money) -> Money | NotFound @post("/prices")
  watchPrice(Lookup) -> Tick | NotFound @get("/prices/{id}/watch") @stream
  live(Tick) -> Tick @ws("/live")
}
`

const everyTargetToml = `
module = "example.com/all/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts-client"
zod = true
react_query = true
msw = true

[generate.ts-server]
out = "./gen/ts-server"

[generate.python-client]
out = "./gen/py"

[generate.dart-client]
out = "./gen/dart"

[generate.rust-client]
out = "./gen/rust"

[generate.rust-server]
out = "./gen/rust"

[generate.openapi]
out = "./gen/openapi"
`

func buildEveryTarget(t *testing.T) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), everyTargetToml)
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "common", "shared.onk"), strictESMSharedOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "catalog", "v1", "service.onk"), everyTargetCatalogOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.onk"), strictESMRuntimeOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	out := map[string][]byte{}
	err := filepath.WalkDir(filepath.Join(dir, "gen"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParallelBuildOfEveryTargetIsDeterministic(t *testing.T) {
	first, second := buildEveryTarget(t), buildEveryTarget(t)
	if len(first) < 40 {
		t.Fatalf("only %d files generated", len(first))
	}
	if len(first) != len(second) {
		t.Fatalf("file counts differ: %d vs %d", len(first), len(second))
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Fatalf("%s differs between two builds of the same schema", name)
		}
	}
}
