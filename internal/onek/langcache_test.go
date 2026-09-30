package onek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func langTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func langCacheProject(t *testing.T, withBroken bool) (string, string, string) {
	t.Helper()
	dir := langTempDir(t)
	good := filepath.Join(dir, "a", "good.onk")
	other := filepath.Join(dir, "b", "other.onk")
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/lc\"\n")
	writeTestFile(t, good, "package a\n\nmessage Item {\n  id: string\n}\n")
	writeTestFile(t, other, "package b\n\nmessage Other {\n  name: string\n}\n")
	paths := []string{good, other}
	if withBroken {
		broken := filepath.Join(dir, "c", "broken.onk")
		writeTestFile(t, broken, "package c\n\nmessage {\n")
		paths = append(paths, broken)
	}
	old := time.Now().Add(-time.Hour)
	for _, path := range paths {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return dir, good, other
}

func hasSymbol(s *LanguageSnapshot, qualified string) bool {
	for _, symbol := range s.Symbols {
		if symbol.QualifiedName == qualified {
			return true
		}
	}
	return false
}

func snapshotJSON(t *testing.T, s *LanguageSnapshot) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseCacheGivesTheSameSnapshotAsAFreshAnalysis(t *testing.T) {
	dir, _, _ := langCacheProject(t, true)
	fresh, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotJSON(t, fresh) != snapshotJSON(t, cached) {
		t.Fatalf("cached analysis differs from the fresh one:\nfresh:  %s\ncached: %s", snapshotJSON(t, fresh), snapshotJSON(t, cached))
	}
	if len(cached.Diagnostics) == 0 {
		t.Fatal("expected the broken file to keep reporting its parse error from the cache")
	}
}

func TestParseCacheNoticesEditedFiles(t *testing.T) {
	dir, good, _ := langCacheProject(t, false)
	if _, err := AnalyzeLanguage(dir, nil); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, good, "package a\n\nmessage Renamed {\n  id: string\n}\n")
	snapshot, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSymbol(snapshot, "a.Renamed") || hasSymbol(snapshot, "a.Item") {
		t.Fatal("analysis served stale content for an edited file")
	}
}

func TestParseCacheRereadsRecentFilesEvenWhenSizeAndMtimeMatch(t *testing.T) {
	dir := langTempDir(t)
	path := filepath.Join(dir, "x.onk")
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/lc\"\n")
	writeTestFile(t, path, "package x\n\nmessage Aaaa {\n  id: string\n}\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzeLanguage(dir, nil); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, "package x\n\nmessage Bbbb {\n  id: string\n}\n")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSymbol(snapshot, "x.Bbbb") {
		t.Fatal("a same-size, same-mtime edit inside the settle window was not noticed")
	}
}

func TestParseCacheStillAppliesOverlaysAndRejectsSymlinks(t *testing.T) {
	dir, good, _ := langCacheProject(t, false)
	if _, err := AnalyzeLanguage(dir, nil); err != nil {
		t.Fatal(err)
	}
	overlay := "package a\n\nmessage FromEditor {\n  id: string\n}\n"
	snapshot, err := AnalyzeLanguage(dir, map[string]string{good: overlay})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSymbol(snapshot, "a.FromEditor") {
		t.Fatal("editor overlay was ignored")
	}
	after, err := AnalyzeLanguage(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSymbol(after, "a.Item") || hasSymbol(after, "a.FromEditor") {
		t.Fatal("the on-disk content must return once the overlay is gone")
	}
}

func TestLanguageServerReusesSnapshotForReadOnlyRequests(t *testing.T) {
	dir, good, _ := langCacheProject(t, false)
	server := &languageServer{root: dir, overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}}
	first, err := server.analyze(false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.analyze(false)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("an unchanged project should reuse the previous snapshot")
	}
	writeTestFile(t, good, "package a\n\nmessage Changed {\n  id: string\n  extra: string\n}\n")
	third, err := server.analyze(false)
	if err != nil {
		t.Fatal(err)
	}
	if third == second || !hasSymbol(third, "a.Changed") {
		t.Fatal("a disk edit without any editor notification must invalidate the snapshot")
	}
	again, err := server.analyze(false)
	if err != nil || again != third {
		t.Fatal("the refreshed snapshot should be reused")
	}
	changed, err := server.analyze(true)
	if err != nil || changed == third {
		t.Fatal("a change notification must always re-analyze")
	}
	if server.analysis.snapshot != nil {
		t.Fatal("a change notification must drop the cached snapshot")
	}
}
