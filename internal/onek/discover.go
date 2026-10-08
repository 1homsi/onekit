package onek

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	onkExtension   = ".onk"
	protoExtension = ".proto"
)

func defaultSchemaExtensions() []string { return []string{onkExtension} }

func discoverOnkFiles(dir string) ([]string, error) {
	return discoverSchemaFiles(dir, nil)
}

func discoverSchemaFiles(dir string, exts []string) ([]string, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	if len(exts) == 0 {
		exts = defaultSchemaExtensions()
	}
	sem := make(chan struct{}, 2*runtime.GOMAXPROCS(0))
	files, err := walkSchemaDir(root, root, sem, exts)
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

func walkSchemaDir(root, dir string, sem chan struct{}, exts []string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	results := make([]schemaWalkResult, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !entry.IsDir() {
			results[i] = checkSchemaEntry(root, path, entry, exts)
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
				files, err := walkSchemaDir(root, path, sem, exts)
				results[i] = schemaWalkResult{files: files, err: err}
			}()
		default:
			files, err := walkSchemaDir(root, path, sem, exts)
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

func checkSchemaEntry(root, path string, d fs.DirEntry, exts []string) schemaWalkResult {
	if d.Type()&os.ModeSymlink != 0 {
		if hasSchemaExtension(path, exts) {
			return schemaWalkResult{err: fmt.Errorf("refusing symlinked schema file: %s", path)}
		}
		return schemaWalkResult{}
	}
	if !hasSchemaExtension(path, exts) {
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
	if strings.HasSuffix(path, protoExtension) {
		isProtobuf, err := looksLikeProtobuf(path)
		if err != nil {
			return schemaWalkResult{err: err}
		}
		if isProtobuf {
			return schemaWalkResult{}
		}
	}
	return schemaWalkResult{files: []string{path}}
}

func hasSchemaExtension(path string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

var (
	protobufSyntaxLine  = regexp.MustCompile(`(?m)^\s*syntax\s*=`)
	protobufFieldNumber = regexp.MustCompile(`(?m)\b[A-Za-z_][A-Za-z0-9_]*\s*=\s*\d+\s*[;\[]`)
)

// looksLikeProtobuf reports whether a .proto file is real Protocol Buffers
// (a syntax declaration, or numbered fields) rather than OneKit schema text
// kept in a file with that extension.
func looksLikeProtobuf(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	head := make([]byte, 64<<10)
	n, err := file.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	text := string(head[:n])
	return protobufSyntaxLine.MatchString(text) || protobufFieldNumber.MatchString(text), nil
}

func isSchemaFileName(path string) bool {
	ext := filepath.Ext(path)
	return ext == onkExtension || ext == protoExtension
}
