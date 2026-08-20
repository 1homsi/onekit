package onek

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxInputFileBytes = 8 << 20
	maxInputFileCount = 4096
)

func canonicalProjectDir(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory %s: %w", dir, err)
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve project directory %s: %w", dir, err)
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", fmt.Errorf("stat project directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path %s is not a directory", dir)
	}
	return filepath.Clean(realPath), nil
}

func readRegularFile(filePath string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlink input %s", filePath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input %s is not a regular file", filePath)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("input %s exceeds the %d-byte limit", filePath, maxBytes)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("input %s exceeds the %d-byte limit", filePath, maxBytes)
	}
	return data, nil
}

// rejectSymlinkPath checks every existing component of a path. It is used
// immediately before directory creation and replacement so a configured
// output cannot silently cross a symlinked parent.
func rejectSymlinkPath(filePath string) error {
	current := filepath.Clean(filePath)
	for {
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing symlink path component %s", current)
			}
		case os.IsNotExist(err):
		default:
			return fmt.Errorf("inspect path %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func validateContainedOutput(root, output string) error {
	if filepath.IsAbs(output) {
		return fmt.Errorf("generator output path %q must be relative to the project", output)
	}
	clean := filepath.Clean(output)
	rel, err := filepath.Rel(root, filepath.Join(root, clean))
	if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("generator output path %q must stay inside the project", output)
	}
	if err := rejectSymlinkPath(filepath.Join(root, clean)); err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Join(root, clean)); err == nil && !info.IsDir() {
		return fmt.Errorf("generator output path %q is not a directory", output)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect generator output path %q: %w", output, err)
	}
	return nil
}

func validateSchemaPath(root, filePath string) error {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return err
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		for _, r := range segment {
			allowed := r == '_' || r == '-' || r == '.'
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !allowed {
				return fmt.Errorf("schema path %s contains unsafe path segment %q", filePath, segment)
			}
		}
	}
	return nil
}
