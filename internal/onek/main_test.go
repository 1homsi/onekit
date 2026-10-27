package onek

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

func TestMain(m *testing.M) {
	cacheDir, err := os.MkdirTemp("", "onek-test-cache-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv(cacheDirEnv, cacheDir)
	compileTypeSpecProject = compileWithTestServer
	code := m.Run()
	_ = os.RemoveAll(cacheDir)
	os.Exit(code)
}

// compileWithTestServer compiles a project's .tsp files with onktest's
// long-running TypeSpec compiler instead of a `tsp compile` process, so a
// test project needs no node_modules and builds in milliseconds.
func compileWithTestServer(root string, files []string) (*onkschema.Document, error) {
	sources := make(map[string]string, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil, err
		}
		sources[filepath.ToSlash(rel)] = string(data)
	}
	doc, err := onktest.Compile(sources)
	var diagnostics *onktest.DiagnosticsError
	if errors.As(err, &diagnostics) {
		return nil, &TypeSpecError{Output: strings.Join(diagnostics.Lines, "\n"), Err: err}
	}
	if err != nil {
		return nil, err
	}
	for i := range doc.Files {
		doc.Files[i].Path = filepath.Join(root, filepath.FromSlash(doc.Files[i].Path))
	}
	return doc, nil
}
