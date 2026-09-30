package onek

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	cacheDir, err := os.MkdirTemp("", "onek-test-cache-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv(cacheDirEnv, cacheDir)
	code := m.Run()
	_ = os.RemoveAll(cacheDir)
	os.Exit(code)
}
