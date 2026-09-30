package interop

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	harnessMu    sync.Mutex
	harnessRoot  string
	harnessPaths = map[string]string{}
)

func cachedHarness(t *testing.T, name string, build func(t *testing.T, dir string) string) string {
	t.Helper()
	harnessMu.Lock()
	if path, ok := harnessPaths[name]; ok {
		harnessMu.Unlock()
		return path
	}
	harnessMu.Unlock()
	dir := filepath.Join(harnessRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create harness dir: %v", err)
	}
	path := build(t, dir)
	harnessMu.Lock()
	harnessPaths[name] = path
	harnessMu.Unlock()
	return path
}

func cargoTargetDir() string {
	return filepath.Join(harnessRoot, "cargo-target")
}

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "onekit-interop-")
	if err != nil {
		panic(err)
	}
	harnessRoot = root
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
