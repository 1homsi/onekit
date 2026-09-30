package onek

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestParallelForVisitsEveryIndexExactlyOnce(t *testing.T) {
	for _, n := range []int{0, 1, 2, 7, 1000} {
		counts := make([]atomic.Int32, n)
		parallelFor(n, func(i int) { counts[i].Add(1) })
		for i := range counts {
			if counts[i].Load() != 1 {
				t.Fatalf("n=%d: index %d visited %d times", n, i, counts[i].Load())
			}
		}
	}
}

func TestParseSourcesKeepsPathOrderAndReportsLowestFailingPath(t *testing.T) {
	dir := discoverTempDir(t)
	var paths []string
	for _, name := range []string{"a", "b", "c", "d"} {
		path := filepath.Join(dir, name+".onk")
		writeTestFile(t, path, "message "+name+" {}\n")
		paths = append(paths, path)
	}
	sources, err := parseSources(paths)
	if err != nil {
		t.Fatal(err)
	}
	for i, source := range sources {
		if source.Path != paths[i] {
			t.Fatalf("source %d is %s, want %s", i, source.Path, paths[i])
		}
	}
	writeTestFile(t, paths[1], "message {\n")
	writeTestFile(t, paths[3], "message {\n")
	_, err = parseSources(paths)
	var diag *ParseDiagnosticError
	if !errors.As(err, &diag) || diag.Path != paths[1] {
		t.Fatalf("expected a parse error for %s, got %v", paths[1], err)
	}
}
