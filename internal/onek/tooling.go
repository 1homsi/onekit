package onek

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/1homsi/onekit/internal/onklang"
)

// FormatError reports the files that would change when formatting in check
// mode. It is intentionally machine-readable for editor and CI integrations.
type FormatError struct {
	Files []string
}

func (e *FormatError) Error() string {
	if len(e.Files) == 0 {
		return "schema files are not formatted"
	}
	return fmt.Sprintf("schema files are not formatted: %s", strings.Join(e.Files, ", "))
}

// Format formats every .onk file under dir. In check mode it never writes and
// returns a FormatError when any file differs from the canonical form.
func Format(dir string, check bool) error {
	paths, err := discoverOnkFiles(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .onk files found under %s", dir)
	}
	var changed []string
	for _, path := range paths {
		data, err := readRegularFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		formatted, err := onklang.Format(string(data))
		if err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		if string(formatted) == string(data) {
			continue
		}
		if len(formatted) == 0 && len(data) > 0 {
			// writeFile treats empty output as "remove stale generated file";
			// formatting must never delete a source schema, so leave files
			// that carry no formattable declarations untouched.
			continue
		}
		if check {
			changed = append(changed, path)
			continue
		}
		if err := writeFile(path, formatted); err != nil {
			return err
		}
	}
	if len(changed) > 0 {
		return &FormatError{Files: changed}
	}
	return nil
}

const initSchema = `package example.api

message HealthRequest {
}

message HealthResponse {
  status: string
}

service HealthService {
  base_path: "/v1"

  health(HealthRequest) -> HealthResponse @get("/health")
}
`

const initConfig = `module = "example.com/myapi"

[generate.go-server]
out = "./gen"

[generate.go-client]
out = "./gen"

[generate.openapi]
out = "./docs"
title = "My API"
version = "0.1.0"
`

// Init creates a small, immediately buildable OneKit project.
func Init(dir string, force bool) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve project directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(root, genDirPerm); err != nil {
		return fmt.Errorf("create project directory %s: %w", root, err)
	}
	root, err = canonicalProjectDir(root)
	if err != nil {
		return err
	}
	files := []struct {
		path    string
		content string
	}{
		{path: filepath.Join(root, configFileName), content: initConfig},
		{path: filepath.Join(root, "api.onk"), content: initSchema},
	}
	for _, file := range files {
		path := file.path
		if info, err := os.Lstat(path); err == nil && !force {
			return fmt.Errorf("refusing to overwrite %s; pass --force to replace it", path)
		} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink target %s", path)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
	}
	for _, file := range files {
		if err := writeFile(file.path, []byte(file.content)); err != nil {
			return fmt.Errorf("write %s: %w", file.path, err)
		}
	}
	return nil
}

type fileStamp struct {
	path   string
	size   int64
	mtime  int64
	digest [sha256.Size]byte
}

func projectSnapshot(dir string) ([]fileStamp, error) {
	paths, err := discoverOnkFiles(dir)
	if err != nil {
		return nil, err
	}
	config := filepath.Join(dir, configFileName)
	if info, statErr := os.Stat(config); statErr == nil && !info.IsDir() {
		paths = append(paths, config)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	sort.Strings(paths)
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		data, err := readRegularFile(path)
		if err != nil {
			return nil, err
		}
		stamps = append(stamps, fileStamp{path: path, size: info.Size(), mtime: info.ModTime().UnixNano(), digest: sha256.Sum256(data)})
	}
	return stamps, nil
}

func sameSnapshot(left, right []fileStamp) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Watch performs an initial build and rebuilds after schema/config changes.
// Build failures are reported and do not terminate the watch, allowing an
// editor to keep working while a file is temporarily incomplete.
func Watch(ctx context.Context, dir string, interval time.Duration, out io.Writer) error {
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	if err := Build(dir); err != nil {
		return err
	}
	previous, err := projectSnapshot(dir)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current, snapshotErr := projectSnapshot(dir)
			if snapshotErr != nil {
				if out != nil {
					_, _ = fmt.Fprintf(out, "onekit: watch snapshot failed: %v\n", snapshotErr)
				}
				continue
			}
			if sameSnapshot(previous, current) {
				continue
			}
			previous = current
			if out != nil {
				_, _ = fmt.Fprintln(out, "onekit: changes detected; rebuilding")
			}
			if buildErr := Build(dir); buildErr != nil && out != nil {
				_, _ = fmt.Fprintf(out, "onekit: build failed: %v\n", buildErr)
			}
		}
	}
}
