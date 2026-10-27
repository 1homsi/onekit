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
)

const initSchema = `import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace example.api;

model HealthRequest {}

model HealthResponse {
  status: string;
}

@route("/v1")
interface HealthService {
  @get @route("/health") health(...HealthRequest): HealthResponse;
}
`

const initConfig = `module = "example.com/%s/gen"

[generate.go-server]
out = "./gen"

[generate.go-client]
out = "./gen"

[generate.openapi]
out = "./docs"
title = "My API"
version = "0.1.0"
`

func initModuleName(dirName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(dirName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-._")
	if name == "" {
		return "myapi"
	}
	return name
}

// Init creates a small, immediately buildable OneKit project.
func Init(dir string, force bool) error {
	return InitTemplateProject(dir, force, "go")
}

func InitTemplateProject(dir string, force bool, template string) error {
	chosen, err := findInitTemplate(template)
	if err != nil {
		return err
	}
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
		{path: filepath.Join(root, configFileName), content: fmt.Sprintf(chosen.config, initModuleName(filepath.Base(root)))},
		{path: filepath.Join(root, typespecEntry), content: chosen.schema},
		{path: filepath.Join(root, "package.json"), content: typespecPackageJSON},
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
	if err := ensureGitignoreEntry(filepath.Join(root, ".gitignore"), ".onekit/"); err != nil {
		return err
	}
	return ensureGitignoreEntry(filepath.Join(root, ".gitignore"), "node_modules/")
}

func ensureGitignoreEntry(path, entry string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed == entry || trimmed == strings.TrimSuffix(entry, "/") {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return writeFile(path, append(data, entry+"\n"...))
}

const stampSettleWindow = 2 * time.Second

type fileStamp struct {
	path     string
	size     int64
	mtime    int64
	hashedAt int64
	digest   [sha256.Size]byte
}

func (f fileStamp) settled() bool {
	return f.hashedAt-f.mtime > int64(stampSettleWindow)
}

func projectSnapshot(dir string, previous []fileStamp) ([]fileStamp, error) {
	schemaRoot, exts, err := resolveSchemaTreeExts(dir)
	if err != nil {
		return nil, err
	}
	paths, err := discoverSchemaFiles(schemaRoot, exts)
	if err != nil {
		return nil, err
	}
	config := configFilePath(dir)
	if info, statErr := os.Stat(config); statErr == nil && !info.IsDir() {
		paths = append(paths, config)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	sort.Strings(paths)
	known := make(map[string]fileStamp, len(previous))
	for _, stamp := range previous {
		known[stamp.path] = stamp
	}
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		hashedAt := time.Now().UnixNano()
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if prior, ok := known[path]; ok && prior.settled() && prior.size == info.Size() && prior.mtime == info.ModTime().UnixNano() {
			stamps = append(stamps, prior)
			continue
		}
		data, err := readRegularFile(path)
		if err != nil {
			return nil, err
		}
		stamps = append(stamps, fileStamp{path: path, size: info.Size(), mtime: info.ModTime().UnixNano(), hashedAt: hashedAt, digest: sha256.Sum256(data)})
	}
	return stamps, nil
}

func sameSnapshot(left, right []fileStamp) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].path != right[i].path || left[i].size != right[i].size || left[i].mtime != right[i].mtime || left[i].digest != right[i].digest {
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
	previous, err := projectSnapshot(dir, nil)
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
			current, snapshotErr := projectSnapshot(dir, previous)
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
