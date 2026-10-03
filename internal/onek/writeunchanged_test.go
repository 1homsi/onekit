package onek

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteFileLeavesIdenticalContentUntouched(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "out", "f.gen.go")
	if err := writeFile(path, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Fatalf("identical content was rewritten: mtime %v, want %v", info.ModTime(), past)
	}
}

func TestWriteFileReplacesChangedContentAndWrongMode(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "f.gen.go")
	if err := writeFile(path, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("package y\n")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "package y\n" {
		t.Fatalf("content = %q", got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("package y\n")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != genFilePerm {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(genFilePerm))
	}
}

func TestBuildSummaryCountsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/api\"\n[generate.go-server]\nout = \"./api\"\n")
	writeTestFile(t, filepath.Join(dir, "a.onk"), "message R {}\nservice S { get(R) -> R @get(\"/r\") }\n")
	first, err := BuildWithSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildWithSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Files == 0 || first.Files != second.Files {
		t.Fatalf("files: first %d, second %d", first.Files, second.Files)
	}
}

func TestWriteFileCreatesNewFilesWithoutLeavingTemporaries(t *testing.T) {
	dir := canonicalTempDir(t)
	path := filepath.Join(dir, "a", "b", "f.gen.go")
	if err := writeFile(path, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("package y\n")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.gen.go" {
		t.Fatalf("unexpected directory contents: %v", entries)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != genFilePerm {
			t.Fatalf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(genFilePerm))
		}
	}
}

func TestWriteFileRefusesSymlinkedOutputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := canonicalTempDir(t)
	target := filepath.Join(dir, "target.go")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live.gen.go")
	dangling := filepath.Join(dir, "dangling.gen.go")
	if err := os.Symlink(target, live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing.go"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{live, dangling} {
		if err := writeFile(link, []byte("package x\n")); err == nil {
			t.Fatalf("%s: writing through a symlink should be refused", link)
		}
		if err := writeFile(link, nil); err == nil {
			t.Fatalf("%s: removing a symlink should be refused", link)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "keep\n" {
		t.Fatalf("symlink target was modified: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.go")); err == nil {
		t.Fatal("dangling symlink target was created")
	}
}

func TestWriteFileEmptyDataRemovesStaleOutput(t *testing.T) {
	dir := canonicalTempDir(t)
	path := filepath.Join(dir, "stale.gen.go")
	if err := writeFile(path, nil); err != nil {
		t.Fatalf("removing a missing file should succeed: %v", err)
	}
	if err := writeFile(path, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale output still present: %v", err)
	}
}

func TestWriteFileRecreatesADirectoryRemovedAfterItWasUsed(t *testing.T) {
	dir := canonicalTempDir(t)
	first := filepath.Join(dir, "out", "a.gen.go")
	if err := writeFile(first, []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(first, []byte("package x\n")); err != nil {
		t.Fatalf("directory removed between writes: %v", err)
	}
}
