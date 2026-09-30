package onek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const buildCacheToml = `
module = "example.com/cached/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts"
`

const buildCacheSchema = `
package api

message Item {
  id: string
  name: string
}

message GetItemRequest {
  id: string
}

service Items {
  base_path: "/items/v1"

  getItem(GetItemRequest) -> Item @get("/items/{id}")
}
`

func cacheProject(t *testing.T) string {
	t.Helper()
	t.Setenv(cacheDirEnv, t.TempDir())
	dir := canonicalTestDir(t)
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), buildCacheToml)
	writeTestFile(t, filepath.Join(dir, "api", "items.onk"), buildCacheSchema)
	return dir
}

func canonicalTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func buildSummary(t *testing.T, dir string) BuildSummary {
	t.Helper()
	summary, err := BuildWithSummary(dir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return summary
}

func readCacheEntry(t *testing.T, dir string) (*buildCacheEntry, string) {
	t.Helper()
	path, ok := buildCachePath(dir)
	if !ok {
		t.Fatal("build cache unexpectedly disabled")
	}
	entry := loadBuildCache(path)
	if entry == nil {
		t.Fatal("no build cache entry was written")
	}
	return entry, path
}

func TestBuildCacheSkipsGenerationWhenNothingChanged(t *testing.T) {
	dir := cacheProject(t)
	first := buildSummary(t, dir)
	if first.Cached || first.Files == 0 {
		t.Fatalf("first build: %+v", first)
	}
	output := filepath.Join(dir, "gen", "go", "api", "types.gen.go")
	before, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	second := buildSummary(t, dir)
	if !second.Cached || second.Files != first.Files {
		t.Fatalf("second build should be served from the cache with the same file count: first=%+v second=%+v", first, second)
	}
	after, err := os.Stat(output)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("cached build touched %s", output)
	}
}

func TestBuildCacheMissesWhenSchemaChanges(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	writeTestFile(t, filepath.Join(dir, "api", "items.onk"), strings.Replace(buildCacheSchema, "name: string", "name: string\n  color: string", 1))
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("schema change was served from the cache")
	}
	types, err := os.ReadFile(filepath.Join(dir, "gen", "go", "api", "types.gen.go"))
	if err != nil || !strings.Contains(string(types), "Color") {
		t.Fatalf("regenerated types are missing the new field: %v", err)
	}
	if summary := buildSummary(t, dir); !summary.Cached {
		t.Fatal("unchanged rebuild after a schema change should hit the cache")
	}
}

func TestBuildCacheMissesWhenConfigChanges(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), buildCacheToml+"\n[generate.openapi]\nout = \"./gen/openapi\"\n")
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("config change was served from the cache")
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "openapi")); err != nil {
		t.Fatalf("new target was not generated: %v", err)
	}
}

func TestBuildCacheRestoresEditedAndDeletedOutputs(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	types := filepath.Join(dir, "gen", "go", "api", "types.gen.go")
	want, err := os.ReadFile(types)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(types, []byte("package api\n// hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("hand-edited output was served from the cache")
	}
	if got, _ := os.ReadFile(types); string(got) != string(want) {
		t.Fatal("hand-edited output was not restored")
	}
	if err := os.Remove(types); err != nil {
		t.Fatal(err)
	}
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("deleted output was served from the cache")
	}
	if got, _ := os.ReadFile(types); string(got) != string(want) {
		t.Fatal("deleted output was not regenerated")
	}
}

func TestBuildCacheCatchesSameSizeEditsInsideTheRacyWindow(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	types := filepath.Join(dir, "gen", "go", "api", "types.gen.go")
	info, err := os.Stat(types)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(types)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(strings.Replace(string(original), "Item", "Itex", 1))
	if len(tampered) != len(original) || string(tampered) == string(original) {
		t.Fatal("tamper must change content but not size")
	}
	if err := os.WriteFile(types, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(types, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("same-size, same-mtime tamper inside the racy window was served from the cache")
	}
	if got, _ := os.ReadFile(types); string(got) != string(original) {
		t.Fatal("tampered output was not restored")
	}
}

func TestBuildCacheIgnoresBuildsFromAnotherExecutable(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	entry, path := readCacheEntry(t, dir)
	entry.ExeHash = strings.Repeat("0", 64)
	entry.ExeMTime++
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("a cache written by a different executable was trusted")
	}
	if summary := buildSummary(t, dir); !summary.Cached {
		t.Fatal("the cache should be rewritten by the current executable")
	}
}

func TestBuildCacheToleratesCorruptEntries(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	_, path := readCacheEntry(t, dir)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("corrupt cache entry was trusted")
	}
	if summary := buildSummary(t, dir); !summary.Cached {
		t.Fatal("cache should be rewritten after a corrupt entry")
	}
}

func TestBuildCacheCanBeDisabled(t *testing.T) {
	dir := cacheProject(t)
	t.Setenv(noCacheEnv, "1")
	buildSummary(t, dir)
	if summary := buildSummary(t, dir); summary.Cached {
		t.Fatal("ONEK_NO_CACHE was ignored")
	}
	if path, ok := buildCachePath(dir); ok || path != "" {
		t.Fatal("cache path should be unavailable when disabled")
	}
}

func TestBuildCacheDoesNotAffectDriftVerification(t *testing.T) {
	dir := cacheProject(t)
	buildSummary(t, dir)
	buildSummary(t, dir)
	if err := VerifyGenerated(dir); err != nil {
		t.Fatalf("VerifyGenerated after cached build: %v", err)
	}
	types := filepath.Join(dir, "gen", "go", "api", "types.gen.go")
	if err := os.WriteFile(types, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerated(dir); err == nil {
		t.Fatal("VerifyGenerated must still report drift")
	}
}
