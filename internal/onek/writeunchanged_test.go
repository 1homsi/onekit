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
