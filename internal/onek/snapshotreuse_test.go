package onek

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func snapshotTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func stampFor(t *testing.T, stamps []fileStamp, path string) fileStamp {
	t.Helper()
	for _, stamp := range stamps {
		if stamp.path == path {
			return stamp
		}
	}
	t.Fatalf("no stamp for %s", path)
	return fileStamp{}
}

func TestProjectSnapshotReusesDigestsOfSettledFiles(t *testing.T) {
	dir := snapshotTempDir(t)
	schema := filepath.Join(dir, "api.onk")
	writeTestFile(t, schema, "message A {}\n")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(schema, old, old); err != nil {
		t.Fatal(err)
	}
	first, err := projectSnapshot(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := projectSnapshot(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSnapshot(first, second) {
		t.Fatal("unchanged project reported as changed")
	}
	if stampFor(t, first, schema).hashedAt != stampFor(t, second, schema).hashedAt {
		t.Fatal("settled file was re-hashed instead of reusing its digest")
	}
}

func TestProjectSnapshotRehashesRecentlyModifiedFiles(t *testing.T) {
	dir := snapshotTempDir(t)
	schema := filepath.Join(dir, "api.onk")
	writeTestFile(t, schema, "message A {}\n")
	info, err := os.Stat(schema)
	if err != nil {
		t.Fatal(err)
	}
	first, err := projectSnapshot(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, schema, "message B {}\n")
	if err := os.Chtimes(schema, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := projectSnapshot(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if sameSnapshot(first, second) {
		t.Fatal("same-size edit with a preserved mtime inside the settle window went undetected")
	}
}

func TestProjectSnapshotDetectsEditsToSettledFiles(t *testing.T) {
	dir := snapshotTempDir(t)
	schema := filepath.Join(dir, "api.onk")
	writeTestFile(t, schema, "message A {}\n")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(schema, old, old); err != nil {
		t.Fatal(err)
	}
	first, err := projectSnapshot(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, schema, "message B {}\n")
	second, err := projectSnapshot(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if sameSnapshot(first, second) {
		t.Fatal("edit to a settled file went undetected")
	}
}

func TestProjectSnapshotDetectsAddedAndRemovedFiles(t *testing.T) {
	dir := snapshotTempDir(t)
	writeTestFile(t, filepath.Join(dir, "a.onk"), "message A {}\n")
	first, err := projectSnapshot(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "b.onk")
	writeTestFile(t, extra, "message B {}\n")
	added, err := projectSnapshot(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if sameSnapshot(first, added) {
		t.Fatal("added file went undetected")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	removed, err := projectSnapshot(dir, added)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSnapshot(first, removed) {
		t.Fatal("removing the added file should restore the original snapshot")
	}
}

func TestSameSnapshotIgnoresHashTime(t *testing.T) {
	left := []fileStamp{{path: "a", size: 1, mtime: 2, hashedAt: 3}}
	right := []fileStamp{{path: "a", size: 1, mtime: 2, hashedAt: 99}}
	if !sameSnapshot(left, right) {
		t.Fatal("hash time must not count as a change")
	}
}
