package onek

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	captureMu sync.Mutex
	captured  map[string][]byte
)

type DriftError struct {
	Files []string
}

func (e *DriftError) Error() string {
	return "generated output is out of date (run onek build): " + strings.Join(e.Files, ", ")
}

func (e *DriftError) ExitCode() int { return 1 }

func VerifyGenerated(dir string) error {
	captureMu.Lock()
	defer captureMu.Unlock()
	captured = map[string][]byte{}
	files := captured
	err := Build(dir)
	captured = nil
	if err != nil {
		return err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	var drift []string
	for path, want := range files {
		if filepath.Base(filepath.Dir(path)) == ".onekit" {
			continue
		}
		have, readErr := os.ReadFile(path)
		switch {
		case len(want) == 0 && readErr == nil:
			drift = append(drift, relativeTo(root, path)+" (stale)")
		case len(want) == 0:
		case readErr != nil:
			drift = append(drift, relativeTo(root, path)+" (missing)")
		case !bytes.Equal(have, want):
			drift = append(drift, relativeTo(root, path))
		}
	}
	if len(drift) == 0 {
		return nil
	}
	sort.Strings(drift)
	return &DriftError{Files: drift}
}

func relativeTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}
