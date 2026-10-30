package onek

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func discoverSchemaFilesSerial(dir string) ([]string, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && skippedSchemaDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if hasSchemaExtension(path) {
				return fmt.Errorf("refusing symlinked schema file: %s", path)
			}
			return nil
		}
		if hasSchemaExtension(path) {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("schema input %s is not a regular file", path)
			}
			if info.Size() > maxInputFileBytes {
				return fmt.Errorf("schema input %s exceeds the %d-byte limit", path, maxInputFileBytes)
			}
			if err := validateSchemaPath(root, path); err != nil {
				return err
			}
			files = append(files, path)
			if len(files) > maxInputFileCount {
				return fmt.Errorf("project contains more than %d schema files", maxInputFileCount)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func discoverTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func touchFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`
model M {}
`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertDiscoverMatchesSerial(t *testing.T, dir string) []string {
	t.Helper()
	want, wantErr := discoverSchemaFilesSerial(dir)
	got, gotErr := discoverProjectSchemaFiles(dir)
	if fmt.Sprint(wantErr) != fmt.Sprint(gotErr) {
		t.Fatalf("error mismatch:\n serial:   %v\n parallel: %v", wantErr, gotErr)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("file list mismatch:\n serial:   %v\n parallel: %v", want, got)
	}
	return got
}

func TestDiscoverSchemaFilesMatchesSerialWalk(t *testing.T) {
	dir := discoverTempDir(t)
	touchFiles(t, dir,
		"a.tsp", "z.tsp", "b/c/d.tsp", "b/c/e.tsp", "b/f.tsp", "a2/x.tsp",
		"b/notes.txt", "node_modules/pkg/skip.tsp", ".hidden/skip.tsp", "vendor/skip.tsp",
		"build/skip.tsp", "deep/1/2/3/4/5/6/leaf.tsp", "deep/1/other.tsp", "dir.tsp/inner.tsp",
	)
	if err := os.Symlink(filepath.Join(dir, "b"), filepath.Join(dir, "linked-dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "b", "notes.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	files := assertDiscoverMatchesSerial(t, dir)
	if len(files) != 9 {
		t.Fatalf("expected 9 schema files, got %d: %v", len(files), files)
	}
}

func TestDiscoverSchemaFilesReportsTheSameFirstErrorAsSerialWalk(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"symlinked schema": func(t *testing.T, dir string) {
			touchFiles(t, dir, "real.tsp", "zz/other.tsp")
			if err := os.Symlink(filepath.Join(dir, "real.tsp"), filepath.Join(dir, "zz", "link.tsp")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		},
		"unsafe name": func(t *testing.T, dir string) {
			touchFiles(t, dir, "ok.tsp", "sub/bad name.tsp")
		},
		"oversized": func(t *testing.T, dir string) {
			touchFiles(t, dir, "ok.tsp", "big/huge.tsp")
			if err := os.Truncate(filepath.Join(dir, "big", "huge.tsp"), maxInputFileBytes+1); err != nil {
				t.Fatal(err)
			}
		},
		"two violations, first wins": func(t *testing.T, dir string) {
			touchFiles(t, dir, "a/bad name.tsp", "m/worse name.tsp", "z/ok.tsp")
		},
		"too many": func(t *testing.T, dir string) {
			for i := 0; i <= maxInputFileCount; i++ {
				touchFiles(t, dir, fmt.Sprintf("d%02d/f%d.tsp", i%40, i))
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := discoverTempDir(t)
			setup(t, dir)
			_, err := discoverProjectSchemaFiles(dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			assertDiscoverMatchesSerial(t, dir)
		})
	}
}

func TestDiscoverSchemaFilesMissingDirectoryErrorsLikeSerialWalk(t *testing.T) {
	dir := filepath.Join(discoverTempDir(t), "missing")
	_, wantErr := discoverSchemaFilesSerial(dir)
	_, gotErr := discoverProjectSchemaFiles(dir)
	if wantErr == nil || gotErr == nil || wantErr.Error() != gotErr.Error() {
		t.Fatalf("serial=%v parallel=%v", wantErr, gotErr)
	}
}
