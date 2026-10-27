package onek

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkschema"
)

const (
	tspExtension = ".tsp"
	// typespecEntry is the file TypeSpec compiles from, at the schema root;
	// every other .tsp file is reached through its imports.
	typespecEntry   = "main.tsp"
	typespecEmitter = "@onkt/typespec"
	// typespecTimeout bounds one `tsp compile`, which normally takes about a
	// second.
	typespecTimeout = 2 * time.Minute
)

// typespecSources compiles the TypeSpec project rooted at root with the
// @onkt/typespec emitter and returns its declarations as compiler sources.
func typespecSources(root string) ([]onkcompile.Source, error) {
	entry := filepath.Join(root, typespecEntry)
	if _, err := os.Stat(entry); err != nil {
		return nil, fmt.Errorf("found .tsp files but no %s in %s; TypeSpec projects compile from %s at the schema root", typespecEntry, root, typespecEntry)
	}
	doc, err := runTypeSpec(root, entry)
	if err != nil {
		return nil, err
	}
	return typespecDocumentSources(root, doc)
}

// typespecDocumentSources turns an emitted schema document into compiler
// sources, checking every path stays inside the schema root.
func typespecDocumentSources(root string, doc *onkschema.Document) ([]onkcompile.Source, error) {
	sources := make([]onkcompile.Source, 0, len(doc.Files))
	for i := range doc.Files {
		file := &doc.Files[i]
		sourcePath, suffix, _ := strings.Cut(file.Path, "#")
		resolved, err := resolveEmittedPath(root, sourcePath)
		if err != nil {
			return nil, err
		}
		path := resolved
		if suffix != "" {
			path += "#" + suffix
		}
		ast := file.File
		sources = append(sources, onkcompile.Source{Path: path, AST: &ast})
	}
	return sources, nil
}

func resolveEmittedPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("TypeSpec emitted declarations for %s: %w", path, err)
	}
	resolved := filepath.Join(dir, filepath.Base(path))
	if err := validateSchemaPath(root, resolved); err != nil {
		return "", fmt.Errorf("TypeSpec emitted declarations for %s, outside the schema root %s; keep every .tsp file the project declares types in under it", path, root)
	}
	return resolved, nil
}

// runTypeSpec runs `tsp compile` on entry with the onkt emitter and decodes
// the schema document it writes.
func runTypeSpec(root, entry string) (*onkschema.Document, error) {
	tsp, err := findTypeSpecCompiler(root)
	if err != nil {
		return nil, err
	}
	out, err := os.MkdirTemp("", "onek-tsp-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(out)

	ctx, cancel := context.WithTimeout(context.Background(), typespecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tsp, "compile", entry,
		"--emit", typespecEmitter,
		"--option", typespecEmitter+".emitter-output-dir="+out,
		"--pretty", "false",
	)
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("tsp compile did not finish within %s", typespecTimeout)
		}
		return nil, &TypeSpecError{Output: typespecDiagnostics(output.String()), Err: err}
	}
	data, err := os.ReadFile(filepath.Join(out, "schema.json"))
	if err != nil {
		return nil, fmt.Errorf("tsp compile wrote no onkt schema (is %s listed in the project's package.json?): %w", typespecEmitter, err)
	}
	return onkschema.DecodeBytes(data)
}

// TypeSpecError is a failed `tsp compile`; Output holds its diagnostics.
type TypeSpecError struct {
	Output string
	Err    error
}

func (e *TypeSpecError) Error() string {
	if e.Output == "" {
		return "tsp compile failed: " + e.Err.Error()
	}
	return "tsp compile failed:\n" + e.Output
}

func (e *TypeSpecError) Unwrap() error { return e.Err }

// typespecDiagnostics drops the compiler's progress lines from its output.
func typespecDiagnostics(output string) string {
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "",
			strings.HasPrefix(trimmed, "TypeSpec compiler v"),
			strings.HasPrefix(trimmed, "- "),
			strings.HasPrefix(trimmed, "✔"),
			strings.HasPrefix(trimmed, "×"):
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// findTypeSpecCompiler finds the project's own `tsp`: node_modules/.bin/tsp
// in the schema root or the nearest directory above it, the way npm scripts
// resolve it. ONEK_TSP overrides the lookup.
func findTypeSpecCompiler(root string) (string, error) {
	if override := os.Getenv("ONEK_TSP"); override != "" {
		return override, nil
	}
	name := "tsp"
	if runtime.GOOS == "windows" {
		name = "tsp.cmd"
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "node_modules", ".bin", name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return "", fmt.Errorf("the TypeSpec compiler is not installed for %s; run `npm install --save-dev @typespec/compiler @typespec/http %s` there", root, typespecEmitter)
}
