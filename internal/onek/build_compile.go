package onek

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkcompat"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func skippedSchemaDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "target", "__pycache__", "dist", "build":
		return true
	}
	return false
}

func parseSources(paths []string) ([]onkcompile.Source, error) {
	sources := make([]onkcompile.Source, len(paths))
	errs := make([]error, len(paths))
	parallelFor(len(paths), func(i int) {
		data, err := readRegularFile(paths[i])
		if err != nil {
			errs[i] = fmt.Errorf("read %s: %w", paths[i], err)
			return
		}
		ast, err := onklang.Parse(string(data))
		if err != nil {
			errs[i] = &ParseDiagnosticError{Path: paths[i], Err: err}
			return
		}
		sources[i] = onkcompile.Source{Path: paths[i], AST: ast}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return sources, nil
}

// repointFileBacklinks retargets every declaration's back link onto dst after
// its declarations have been merged into a new onkir.File.
func repointFileBacklinks(dst *onkir.File) {
	for _, m := range dst.Messages {
		m.File = dst
	}
	for _, e := range dst.Enums {
		e.File = dst
	}
	for _, s := range dst.Services {
		s.File = dst
	}
}

// relDirOf returns a compiled file's source directory relative to the schema
// root (the directory containing onekit.toml), in slash form. "." means the
// file sits at the schema root itself.
func relDirOf(schemaRoot string, f *onkir.File) (string, error) {
	rel, err := filepath.Rel(schemaRoot, filepath.Dir(f.Path))
	if err != nil {
		return "", fmt.Errorf("compute relative dir for %s: %w", f.Path, err)
	}
	return filepath.ToSlash(rel), nil
}

// applyDefaultBasePaths fills in Service.BasePath for every service that
// didn't set one explicitly, inferring it from the service's source
// directory (see inferBasePath). An explicit base_path always wins. The
// degenerate root inference "/" is stored as "" so route concatenation
// cannot produce invalid "//path" patterns (Go's ServeMux panics on them).
func applyDefaultBasePaths(pkg *onkir.Package, schemaRoot string) error {
	for _, f := range pkg.Files {
		if len(f.Services) == 0 {
			continue
		}
		rel, err := relDirOf(schemaRoot, f)
		if err != nil {
			return err
		}
		for _, svc := range f.Services {
			if svc.BasePath == "" {
				svc.BasePath = normalizeBasePath(inferBasePath(rel))
			}
		}
	}
	return nil
}

// normalizeBasePath collapses a bare "/" base path to "" - both mean "serve
// at the root", but only "" keeps BasePath+route concatenation well-formed.
func normalizeBasePath(basePath string) string {
	if basePath == "/" {
		return ""
	}
	return basePath
}

// applyRoutePrefix prepends the configured public HTTP prefix after service
// base paths have been resolved. Applying it to the shared IR keeps every
// generator backend in lockstep while leaving package layout and imports
// relative to the schema root.
func applyRoutePrefix(pkg *onkir.Package, prefix string) {
	for _, f := range pkg.Files {
		for _, service := range f.Services {
			switch {
			case prefix != "":
				service.BasePath = prefix + normalizeBasePath(service.BasePath)
			default:
				service.BasePath = normalizeBasePath(service.BasePath)
			}
		}
	}
}

// sourceGroup is one compiled schema directory: every .onk file directly
// under it, merged into a single onkir.File for generation. Each directory
// maps 1:1 to one generated Go/TS/Python package - the same "one service per
// directory" convention already used by every migrated proto-based service.
type sourceGroup struct {
	relDir string
	file   *onkir.File
}

// sourceIndex groups a compiled package's files by directory and separately
// tracks which directory each message/enum originally came from, since
// grouping consolidates them into new merged onkir.Files (see sourceGroup)
// that no longer reflect the original per-file source layout.
type sourceIndex struct {
	groups       []*sourceGroup
	dirByMessage map[*onkir.Message]string
	dirByEnum    map[*onkir.Enum]string
}

func (idx *sourceIndex) hasWS() bool {
	for _, g := range idx.groups {
		if onkir.FileHasWSMethods(g.file) {
			return true
		}
	}
	return false
}

func indexMessage(idx *sourceIndex, m *onkir.Message, relDir string) {
	idx.dirByMessage[m] = relDir
	for _, nested := range m.Nested {
		indexMessage(idx, nested, relDir)
	}
	for _, nested := range m.NestedEnums {
		idx.dirByEnum[nested] = relDir
	}
}

func groupByDirectory(pkg *onkir.Package, schemaRoot string) (*sourceIndex, error) {
	idx := &sourceIndex{
		dirByMessage: map[*onkir.Message]string{},
		dirByEnum:    map[*onkir.Enum]string{},
	}

	byDir := map[string]*onkir.File{}
	var order []string
	for _, f := range pkg.Files {
		rel, err := relDirOf(schemaRoot, f)
		if err != nil {
			return nil, err
		}

		for _, m := range f.Messages {
			indexMessage(idx, m, rel)
		}
		for _, e := range f.Enums {
			idx.dirByEnum[e] = rel
		}

		merged, ok := byDir[rel]
		if !ok {
			merged = &onkir.File{}
			byDir[rel] = merged
			order = append(order, rel)
		}
		merged.Messages = append(merged.Messages, f.Messages...)
		merged.Enums = append(merged.Enums, f.Enums...)
		merged.Services = append(merged.Services, f.Services...)
	}

	sort.Strings(order)
	for _, rel := range order {
		f := byDir[rel]
		repointFileBacklinks(f)
		idx.groups = append(idx.groups, &sourceGroup{relDir: rel, file: f})
	}
	return idx, nil
}

// Compile parses and compiles every .onk file under dir without generating
// output. It also applies the same default service base paths used by Build.
func Compile(dir string) (*onkir.Package, error) {
	return CompileWithOptions(dir, onkcompile.CompileOptions{})
}

// CompileWithOptions parses and compiles every .onk file under dir with the
// requested compatibility behavior. It also applies the same default
// service base paths used by Build.
func CompileWithOptions(dir string, options onkcompile.CompileOptions) (*onkir.Package, error) {
	return compileTree(dir, nil, options)
}

func compileTree(dir string, exts []string, options onkcompile.CompileOptions) (*onkir.Package, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	files, err := discoverSchemaFiles(root, exts)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .onk files found under %s", dir)
	}
	sources, err := parseSources(files)
	if err != nil {
		return nil, err
	}
	pkg, err := onkcompile.CompileWithOptions(sources, options)
	if err != nil {
		return nil, err
	}
	if err := applyDefaultBasePaths(pkg, root); err != nil {
		return nil, err
	}
	return pkg, nil
}

// errNoConfigFile is returned by loadOptionalConfig when the project
// directory has no onekit.toml; callers treat it as "use defaults".
var errNoConfigFile = errors.New("no onekit.toml in project directory")

// loadOptionalConfig loads onekit.toml when present and returns
// errNoConfigFile when the project has no config file.
func loadOptionalConfig(dir string) (*Config, error) {
	configPath := filepath.Join(dir, configFileName)
	if _, statErr := os.Stat(configPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, errNoConfigFile
		}
		return nil, fmt.Errorf("stat %s: %w", configPath, statErr)
	}
	return LoadConfig(dir)
}

// resolveSchemaTree returns the effective root of the .onk schema tree for
// a project directory: the configured schema_root when onekit.toml declares
// one, otherwise the directory itself.
func resolveSchemaTree(dir string) (string, error) {
	root, _, err := resolveSchemaTreeExts(dir)
	return root, err
}

func resolveSchemaTreeExts(dir string) (string, []string, error) {
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return "", nil, err
	}
	if cfg != nil {
		return cfg.SchemaDir(), cfg.schemaExtensions(), nil
	}
	return dir, nil, nil
}

// Check validates the project configuration and every schema under the
// project's schema tree (see Config.SchemaRoot) without generating output.
func Check(dir string) error {
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return err
	}
	root := dir
	if cfg != nil {
		root = cfg.SchemaDir()
	}
	return checkAt(root, cfg)
}

func checkAt(root string, cfg *Config) error {
	_, err := compileTree(root, cfg.schemaExtensions(), cfg.CompileOptions())
	return err
}

// Compatibility compares two schema directories and returns breaking changes.
func Compatibility(previousDir, currentDir string) ([]onkcompat.Finding, error) {
	currentCfg, err := loadOptionalConfig(currentDir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return nil, fmt.Errorf("compile current schema: %w", err)
	}
	previous, err := compileCompatibilityProject(previousDir, currentCfg)
	if err != nil {
		return nil, fmt.Errorf("compile previous schema: %w", err)
	}
	current, err := compileCompatibilityProject(currentDir, nil)
	if err != nil {
		return nil, fmt.Errorf("compile current schema: %w", err)
	}
	return onkcompat.Compare(previous, current), nil
}

func compileCompatibilityProject(dir string, fallback *Config) (*onkir.Package, error) {
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return nil, err
	}
	root := dir
	options := onkcompile.CompileOptions{}
	routePrefix := ""
	var exts []string
	switch {
	case cfg != nil:
		root = cfg.SchemaDir()
		options = cfg.CompileOptions()
		routePrefix = cfg.RoutePrefix
		exts = cfg.schemaExtensions()
	case fallback != nil:
		options = fallback.CompileOptions()
		routePrefix = fallback.RoutePrefix
		exts = fallback.schemaExtensions()
		if fallback.SchemaRoot != "" {
			if info, statErr := os.Stat(filepath.Join(dir, fallback.SchemaRoot)); statErr == nil && info.IsDir() {
				root = filepath.Join(dir, fallback.SchemaRoot)
			}
		}
	}
	pkg, err := compileTree(root, exts, options)
	if err != nil {
		return nil, err
	}
	applyRoutePrefix(pkg, routePrefix)
	return pkg, nil
}
