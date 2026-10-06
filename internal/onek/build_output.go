package onek

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	genDirPerm  = 0o755
	genFilePerm = 0o644
)

func writeFile(path string, data []byte) error {
	if captured != nil {
		captureWriteMu.Lock()
		captured[path] = data
		captureWriteMu.Unlock()
		return nil
	}
	dir := filepath.Dir(path)
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	if len(data) == 0 {
		return removeStaleOutput(path)
	}
	if filepath.Base(dir) != ".onekit" {
		writtenFiles.Add(1)
	}
	if err := ensureOutputDir(dir); err != nil {
		return err
	}
	created, err := createNewOutput(path, data)
	if errors.Is(err, fs.ErrNotExist) {
		createdOutputDirs.Delete(dir)
		if err = ensureOutputDir(dir); err != nil {
			return err
		}
		created, err = createNewOutput(path, data)
	}
	if err != nil {
		return err
	}
	if created {
		recordOutput(path, data)
		return nil
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing symlink path component %s", path)
	case err == nil && unchangedOnDisk(path, info, data):
		recordOutput(path, data)
		return nil
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	return replaceOutput(path, dir, data)
}

func removeStaleOutput(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing symlink path component %s", path)
	case err != nil && os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale generated output %s: %w", path, err)
	}
	return nil
}

func ensureOutputDir(dir string) error {
	if _, ok := createdOutputDirs.Load(dir); ok {
		return nil
	}
	if err := os.MkdirAll(dir, genDirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	createdOutputDirs.Store(dir, struct{}{})
	return nil
}

func createNewOutput(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, genFilePerm)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	fail := func(step string, cause error) (bool, error) {
		_ = f.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("%s %s: %w", step, path, cause)
	}
	if err := ensureModeAfterCreate(f); err != nil {
		return fail("set permissions on", err)
	}
	if _, err := f.Write(data); err != nil {
		return fail("write", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("close %s: %w", path, err)
	}
	return true, nil
}

func ensureModeAfterCreate(f *os.File) error {
	if createModeTrusted.Load() {
		return nil
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() == genFilePerm {
		createModeTrusted.Store(true)
		return nil
	}
	return f.Chmod(genFilePerm)
}

func replaceOutput(path, dir string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".onek-*")
	if err != nil {
		return fmt.Errorf("create temporary output for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(genFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set permissions on temporary output for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary output for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	renamed = true
	recordOutput(path, data)
	return nil
}

var (
	createdOutputDirs sync.Map
	createModeTrusted atomic.Bool
)

func eachGroup(idx *sourceIndex, fn func(*sourceGroup) error) error {
	errs := make([]error, len(idx.groups))
	limit := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, g := range idx.groups {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			errs[i] = fn(g)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func unchangedOnDisk(path string, info os.FileInfo, data []byte) bool {
	if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != genFilePerm {
		return false
	}
	existing, err := os.ReadFile(path)
	return err == nil && bytes.Equal(existing, data)
}

func lastPathSegment(p string) string {
	p = strings.TrimSuffix(p, "/")
	parts := strings.Split(filepath.ToSlash(p), "/")
	return parts[len(parts)-1]
}

func goPackageIdent(segment string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(segment) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func groupOutDir(outRoot, relDir string) string {
	return filepath.Join(outRoot, filepath.FromSlash(relDir))
}
