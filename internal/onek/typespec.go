package onek

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkschema"
)

const (
	tspExtension = ".tsp"
	// typespecEntry is the file TypeSpec compiles from when a project has
	// one at its schema root; without it every .tsp file is compiled.
	typespecEntry   = "main.tsp"
	typespecEmitter = "@onkt/typespec"
	// typespecTimeout bounds one `tsp compile`, which normally takes about a
	// second.
	typespecTimeout = 2 * time.Minute
)

// compileTypeSpecProject compiles the TypeSpec project rooted at root and
// returns its schema document. Tests replace it with an in-process compiler.
var compileTypeSpecProject = runTypeSpecProject

// runTypeSpecProject runs the project's own `tsp compile`.
func runTypeSpecProject(root string, files []string) (*onkschema.Document, error) {
	entry := filepath.Join(root, typespecEntry)
	if _, err := os.Stat(entry); err == nil {
		return runTypeSpec(root, entry)
	}
	return runTypeSpecFiles(root, files)
}

// typespecSources compiles the TypeSpec project rooted at root with the
// @onkt/typespec emitter and returns its declarations as compiler sources.
func typespecSources(root string, files []string) ([]onkcompile.Source, error) {
	doc, err := compileTypeSpecProject(root, files)
	if err != nil {
		return nil, err
	}
	return typespecDocumentSources(root, doc)
}

// runTypeSpecFiles compiles a project without a main.tsp from a generated
// entry that imports every .tsp file under the schema root.
func runTypeSpecFiles(root string, files []string) (*onkschema.Document, error) {
	dir, err := os.MkdirTemp("", "onkt-entry-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	var b strings.Builder
	for _, file := range files {
		fmt.Fprintf(&b, "import %q;\n", filepath.ToSlash(file))
	}
	entry := filepath.Join(dir, typespecEntry)
	if err := os.WriteFile(entry, []byte(b.String()), genFilePerm); err != nil {
		return nil, err
	}
	return runTypeSpec(root, entry)
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
	out, err := os.MkdirTemp("", "onkt-tsp-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(out)

	ctx, cancel := context.WithTimeout(context.Background(), typespecTimeout)
	defer cancel()
	args := append(slices.Clone(tsp[1:]), "compile", entry,
		"--emit", findEmitter(root),
		"--option", typespecEmitter+".emitter-output-dir="+out,
		"--pretty", "false",
	)
	cmd := exec.CommandContext(ctx, tsp[0], args...) //nolint:gosec // the project's own TypeSpec compiler
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

// findEmitter is the project's installed @onkt/typespec, found the way npm
// resolves packages from the schema root. TypeSpec would otherwise resolve
// it from the entry file, which may be a generated one outside the project.
func findEmitter(root string) string {
	for dir := root; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "node_modules", filepath.FromSlash(typespecEmitter))
		if info, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil && !info.IsDir() {
			return candidate
		}
		if filepath.Dir(dir) == dir {
			return typespecEmitter
		}
	}
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

// typespecDiagnostic matches one `tsp compile --pretty false` diagnostic:
// "path:line:col - error code: message".
var typespecDiagnostic = regexp.MustCompile(`^(.+?):(\d+):(\d+) - (error|warning) (\S+): (.*)$`)

// Diagnostics parses the compiler output into structured diagnostics.
func (e *TypeSpecError) Diagnostics() []Diagnostic {
	var diagnostics []Diagnostic
	for _, line := range strings.Split(e.Output, "\n") {
		match := typespecDiagnostic.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil || match[4] != "error" {
			continue
		}
		lineNumber, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		diagnostics = append(diagnostics, Diagnostic{Path: match[1], Line: lineNumber, Column: column, Code: match[5], Message: match[6]})
	}
	return diagnostics
}

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

// findTypeSpecCompiler finds the project's own TypeSpec compiler, in the
// node_modules of the schema root or the nearest directory above it, the way
// npm scripts resolve it, and returns the command that runs it. The compiler
// runs under node directly: on Windows the node_modules/.bin/tsp.cmd shim
// goes through cmd.exe, which splits a project path that holds a space.
// ONKT_TSP overrides the lookup.
func findTypeSpecCompiler(root string) ([]string, error) {
	if override := os.Getenv("ONKT_TSP"); override != "" {
		return []string{override}, nil
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		script := filepath.Join(dir, "node_modules", "@typespec", "compiler", "cmd", "tsp.js")
		if info, err := os.Stat(script); err == nil && !info.IsDir() {
			node, err := exec.LookPath("node")
			if err != nil {
				return nil, errors.New("node is not on PATH; onkt compiles TypeSpec with Node.js")
			}
			return []string{node, script}, nil
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return nil, errors.New("the TypeSpec compiler is not installed: " + typespecInstallHint(root))
}

// typespecInstallHint says where to install the TypeSpec toolchain: in the
// directory of the package.json that governs the schema root, which is the
// project root rather than a schemas directory below it.
func typespecInstallHint(root string) string {
	install := "npm install --save-dev @typespec/compiler @typespec/http " + typespecEmitter
	for dir := root; ; dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			if strings.Contains(string(data), `"@typespec/compiler"`) {
				return fmt.Sprintf("run `npm install` in %s", dir)
			}
			return fmt.Sprintf("run `%s` in %s", install, dir)
		}
		if filepath.Dir(dir) == dir {
			return fmt.Sprintf("there is no package.json above %s; run `%s` in the project directory, beside %s", root, install, configFileName)
		}
	}
}
