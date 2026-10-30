package onek

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

func discoverProjectSchemaFiles(dir string) ([]string, error) {
	return discoverSchemaFiles(dir)
}

// discoveryMemo keeps the schema files found under each root for the length
// of one build, which otherwise walks the tree for the fingerprint, the
// compile and the manifest.
var discoveryMemo struct {
	sync.Mutex
	active  bool
	results map[string][]string
}

func startDiscoveryMemo() {
	discoveryMemo.Lock()
	discoveryMemo.active = true
	discoveryMemo.results = map[string][]string{}
	discoveryMemo.Unlock()
}

func stopDiscoveryMemo() {
	discoveryMemo.Lock()
	discoveryMemo.active = false
	discoveryMemo.results = nil
	discoveryMemo.Unlock()
}

func discoverSchemaFiles(dir string) ([]string, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	key := root
	discoveryMemo.Lock()
	if files, ok := discoveryMemo.results[key]; ok && discoveryMemo.active {
		discoveryMemo.Unlock()
		return append([]string(nil), files...), nil
	}
	discoveryMemo.Unlock()
	files, err := walkSchemaFiles(root, dir)
	if err != nil {
		return nil, err
	}
	discoveryMemo.Lock()
	if discoveryMemo.active {
		discoveryMemo.results[key] = append([]string(nil), files...)
	}
	discoveryMemo.Unlock()
	return files, nil
}

func walkSchemaFiles(root, dir string) ([]string, error) {
	sem := make(chan struct{}, 2*runtime.GOMAXPROCS(0))
	files, err := walkSchemaDir(root, root, sem)
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	if len(files) > maxInputFileCount {
		return nil, fmt.Errorf("walk %s: project contains more than %d schema files", dir, maxInputFileCount)
	}
	sort.Strings(files)
	return files, nil
}

type schemaWalkResult struct {
	files []string
	err   error
}

func walkSchemaDir(root, dir string, sem chan struct{}) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	results := make([]schemaWalkResult, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !entry.IsDir() {
			results[i] = checkSchemaEntry(root, path, entry)
			continue
		}
		if skippedSchemaDir(entry.Name()) {
			continue
		}
		select {
		case sem <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				files, err := walkSchemaDir(root, path, sem)
				results[i] = schemaWalkResult{files: files, err: err}
			}()
		default:
			files, err := walkSchemaDir(root, path, sem)
			results[i] = schemaWalkResult{files: files, err: err}
		}
	}
	wg.Wait()
	var files []string
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		files = append(files, result.files...)
	}
	return files, nil
}

func checkSchemaEntry(root, path string, d fs.DirEntry) schemaWalkResult {
	if d.Type()&os.ModeSymlink != 0 {
		if hasSchemaExtension(path) {
			return schemaWalkResult{err: fmt.Errorf("refusing symlinked schema file: %s", path)}
		}
		return schemaWalkResult{}
	}
	if !hasSchemaExtension(path) {
		return schemaWalkResult{}
	}
	info, err := d.Info()
	if err != nil {
		return schemaWalkResult{err: err}
	}
	if !info.Mode().IsRegular() {
		return schemaWalkResult{err: fmt.Errorf("schema input %s is not a regular file", path)}
	}
	if info.Size() > maxInputFileBytes {
		return schemaWalkResult{err: fmt.Errorf("schema input %s exceeds the %d-byte limit", path, maxInputFileBytes)}
	}
	if err := validateSchemaPath(root, path); err != nil {
		return schemaWalkResult{err: err}
	}
	return schemaWalkResult{files: []string{path}}
}

func hasSchemaExtension(path string) bool {
	return strings.HasSuffix(path, tspExtension)
}
