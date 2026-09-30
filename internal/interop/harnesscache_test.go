package interop

import (
	"os"
	"sync"
	"testing"
)

var (
	harnessMu    sync.Mutex
	harnessPaths = map[string]string{}
	harnessDirs  []string
)

func cachedHarness(t *testing.T, name string, build func(t *testing.T, dir string) string) string {
	t.Helper()
	harnessMu.Lock()
	if path, ok := harnessPaths[name]; ok {
		harnessMu.Unlock()
		return path
	}
	dir, err := os.MkdirTemp("", "onekit-interop-"+name+"-")
	if err != nil {
		harnessMu.Unlock()
		t.Fatalf("create harness dir: %v", err)
	}
	harnessDirs = append(harnessDirs, dir)
	harnessMu.Unlock()
	path := build(t, dir)
	harnessMu.Lock()
	harnessPaths[name] = path
	harnessMu.Unlock()
	return path
}

func TestMain(m *testing.M) {
	code := m.Run()
	for _, dir := range harnessDirs {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
