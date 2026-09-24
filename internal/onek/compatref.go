package onek

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/1homsi/onekit/internal/onkcompat"
)

func CompatibilityAgainstRef(ref, currentDir string) ([]onkcompat.Finding, error) {
	absDir, err := filepath.Abs(currentDir)
	if err != nil {
		return nil, err
	}
	top, err := gitOutput(absDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("find git repository for %s: %w", currentDir, err)
	}
	top = strings.TrimSpace(top)
	if resolved, resolveErr := filepath.EvalSymlinks(absDir); resolveErr == nil {
		absDir = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(top); resolveErr == nil {
		top = resolved
	}
	rel, err := filepath.Rel(top, absDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("%s is not inside the git repository %s", currentDir, top)
	}
	archive, err := gitOutput(top, "archive", "--format=tar", ref, "--", filepath.ToSlash(rel))
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", rel, ref, err)
	}
	baseline, err := os.MkdirTemp("", "onek-compat-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(baseline) }()
	if err := extractTar(bytes.NewReader([]byte(archive)), baseline); err != nil {
		return nil, err
	}
	return Compatibility(filepath.Join(baseline, rel), currentDir)
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func extractTar(r io.Reader, dest string) error {
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(header.Name))
		if !pathWithin(dest, target) {
			return fmt.Errorf("archive entry %q escapes the baseline directory", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, genDirPerm); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), genDirPerm); err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(reader, 64<<20))
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, genFilePerm); err != nil {
				return err
			}
		}
	}
}
