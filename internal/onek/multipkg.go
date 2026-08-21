package onek

import (
	"path/filepath"
	"strings"

	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkir"
)

// tsImportPath computes the relative TS import specifier from the module
// currently being generated (fromRelDir) to another schema directory
// (toRelDir), e.g. from "hub/business/v1" to "common" ->
// "../../../common/types". Every generated TS package writes its types to a
// file literally named types.ts, so the target file name is fixed.
func tsImportPath(fromRelDir, toRelDir string) string {
	rel, err := filepath.Rel(filepath.FromSlash(fromRelDir), filepath.FromSlash(toRelDir))
	if err != nil {
		rel = toRelDir
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel + "/types"
}

// tsResolver implements gents.PackageResolver by looking up which schema
// directory produced a given message/enum, and treating anything outside the
// group currently being generated as external.
type tsResolver struct {
	currentDir string
	idx        *sourceIndex
}

func (r *tsResolver) resolve(dir string, ok bool) (gents.PackageRef, bool) {
	if !ok || dir == r.currentDir {
		return gents.PackageRef{}, false
	}
	return gents.PackageRef{Alias: goPackageAlias(dir), ImportPath: tsImportPath(r.currentDir, dir)}, true
}

func (r *tsResolver) ResolveMessage(m *onkir.Message) (gents.PackageRef, bool) {
	dir, ok := r.idx.dirByMessage[m]
	return r.resolve(dir, ok)
}

func (r *tsResolver) ResolveEnum(e *onkir.Enum) (gents.PackageRef, bool) {
	dir, ok := r.idx.dirByEnum[e]
	return r.resolve(dir, ok)
}

// pyModulePath computes a schema directory's dotted Python module path for
// its generated models module, e.g. "hub/business/v1" -> "hub.business.v1.models",
// or "models" for the schema root itself. Used both for a package's own
// `from {this} import ...` and for another package's cross-module import.
func pyModulePath(relDir string) string {
	if relDir == "." || relDir == "" {
		return "models"
	}
	parts := strings.Split(filepath.ToSlash(relDir), "/")
	for i, part := range parts {
		parts[i] = pythonModuleSegment(part)
	}
	return strings.Join(parts, ".") + ".models"
}

func pythonModuleSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	segment := b.String()
	if segment == "" {
		segment = "pkg"
	}
	if segment[0] >= '0' && segment[0] <= '9' {
		segment = "pkg_" + segment
	}
	if pythonKeywords[segment] {
		segment += "_"
	}
	return segment
}

// pythonKeywordBase is Python's hard keyword list in canonical spelling.
// The module-path guard below derives from it so it can never drift from the
// language. Member-name rejection lives in internal/onkcompile
// (pythonMemberKeywords), which matches these hard keywords
// case-insensitively plus the JSON literal look-alikes.
var pythonKeywordBase = []string{
	"False", "None", "True", "and", "as", "assert", "async", "await", "break",
	"class", "continue", "def", "del", "elif", "else", "except", "finally",
	"for", "from", "global", "if", "import", "in", "is", "lambda", "nonlocal",
	"not", "or", "pass", "raise", "return", "try", "while", "with", "yield",
}

// pythonKeywords guards generated module path segments, which preserve their
// declared casing, so membership is probed with exact spelling. The soft
// keywords are also rejected here to keep generated import lines conservative.
var pythonKeywords = func() map[string]bool {
	m := map[string]bool{"match": true, "case": true}
	for _, kw := range pythonKeywordBase {
		m[kw] = true
	}
	return m
}()

// pyResolver implements genpy.PackageResolver the same way tsResolver does
// for TypeScript, using absolute dotted module paths instead of relative
// import specifiers - Python doesn't need a relative path since the whole
// generated tree is one package rooted at the python-client out directory
// (see writePythonInitFiles).
type pyResolver struct {
	currentDir string
	idx        *sourceIndex
}

func (r *pyResolver) resolve(dir string, ok bool) (genpy.PackageRef, bool) {
	if !ok || dir == r.currentDir {
		return genpy.PackageRef{}, false
	}
	return genpy.PackageRef{Alias: goPackageAlias(dir) + "_models", ModulePath: pyModulePath(dir)}, true
}

func (r *pyResolver) ResolveMessage(m *onkir.Message) (genpy.PackageRef, bool) {
	dir, ok := r.idx.dirByMessage[m]
	return r.resolve(dir, ok)
}

func (r *pyResolver) ResolveEnum(e *onkir.Enum) (genpy.PackageRef, bool) {
	dir, ok := r.idx.dirByEnum[e]
	return r.resolve(dir, ok)
}

// writePythonInitFiles creates an empty __init__.py at outRoot and at every
// directory level down to outRoot/relDir, so the generated tree forms a real
// Python package - required for the absolute dotted imports pyResolver emits
// to actually resolve.
func writePythonInitFiles(outRoot, relDir string) error {
	const initStub = "# Code generated by onek. DO NOT EDIT.\n"

	if err := writeFile(filepath.Join(outRoot, "__init__.py"), []byte(initStub)); err != nil {
		return err
	}
	if relDir == "." || relDir == "" {
		return nil
	}

	current := outRoot
	for _, seg := range strings.Split(filepath.ToSlash(relDir), "/") {
		current = filepath.Join(current, seg)
		if err := writeFile(filepath.Join(current, "__init__.py"), []byte(initStub)); err != nil {
			return err
		}
	}
	return nil
}

// rustRelativeTypesModule returns a module path from a generated types.rs,
// client.rs, or server.rs file to another schema directory's types module.
// Every generated source file is one module below its schema directory, so
// the path first climbs out of that file module, then to the common schema
// ancestor, before descending to the target.
func rustRelativeTypesModule(fromRelDir, toRelDir string) string {
	from := rustPathSegments(fromRelDir)
	to := rustPathSegments(toRelDir)
	common := 0
	for common < len(from) && common < len(to) && from[common] == to[common] {
		common++
	}
	parts := make([]string, 0, len(from)-common+len(to)-common+2)
	for range len(from) - common + 1 {
		parts = append(parts, "super")
	}
	for _, segment := range to[common:] {
		parts = append(parts, genrust.RustIdent(segment))
	}
	parts = append(parts, "types")
	return strings.Join(parts, "::")
}

type rustResolver struct {
	currentDir string
	idx        *sourceIndex
}

func (r *rustResolver) resolve(dir string, ok bool) (genrust.PackageRef, bool) {
	if !ok || dir == r.currentDir {
		return genrust.PackageRef{}, false
	}
	return genrust.PackageRef{ModulePath: rustRelativeTypesModule(r.currentDir, dir)}, true
}

func (r *rustResolver) ResolveMessage(message *onkir.Message) (genrust.PackageRef, bool) {
	dir, ok := r.idx.dirByMessage[message]
	return r.resolve(dir, ok)
}

func (r *rustResolver) ResolveEnum(enum *onkir.Enum) (genrust.PackageRef, bool) {
	dir, ok := r.idx.dirByEnum[enum]
	return r.resolve(dir, ok)
}
