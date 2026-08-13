package onek

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/genopenapi"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkcompat"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func discoverOnkFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".onk") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func parseSources(paths []string) ([]onkcompile.Source, error) {
	var sources []onkcompile.Source
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		ast, err := onklang.Parse(string(data))
		if err != nil {
			return nil, &ParseDiagnosticError{Path: path, Err: err}
		}
		sources = append(sources, onkcompile.Source{Path: path, AST: ast})
	}
	return sources, nil
}

// mergeFiles combines every compiled onkir.File in a package into a single
// File, regardless of source directory. Used for OpenAPI generation, which
// stays one combined document across the whole schema tree (a single API
// surface, not one document per service) rather than following the
// per-directory split used for Go/TS/Python output - see sourceIndex.
func mergeFiles(pkg *onkir.Package, goPackage string) *onkir.File {
	merged := &onkir.File{Package: goPackage}
	for _, f := range pkg.Files {
		merged.Messages = append(merged.Messages, f.Messages...)
		merged.Enums = append(merged.Enums, f.Enums...)
		merged.Services = append(merged.Services, f.Services...)
	}
	for _, m := range merged.Messages {
		m.File = merged
	}
	for _, e := range merged.Enums {
		e.File = merged
	}
	for _, s := range merged.Services {
		s.File = merged
	}
	return merged
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
// directory (see inferBasePath). An explicit base_path always wins.
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
				svc.BasePath = inferBasePath(rel)
			}
		}
	}
	return nil
}

// applyRoutePrefix prepends the configured public HTTP prefix after service
// base paths have been resolved. Applying it to the shared IR keeps every
// generator backend in lockstep while leaving package layout and imports
// relative to the schema root.
func applyRoutePrefix(pkg *onkir.Package, prefix string) {
	if prefix == "" {
		return
	}
	for _, f := range pkg.Files {
		for _, service := range f.Services {
			if service.BasePath == "/" {
				service.BasePath = prefix
				continue
			}
			service.BasePath = prefix + service.BasePath
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
		for _, m := range f.Messages {
			m.File = f
		}
		for _, e := range f.Enums {
			e.File = f
		}
		for _, s := range f.Services {
			s.File = f
		}
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
	files, err := discoverOnkFiles(dir)
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
	if err := applyDefaultBasePaths(pkg, dir); err != nil {
		return nil, err
	}
	return pkg, nil
}

// Check validates the project configuration and every schema under dir
// without generating output.
func Check(dir string) error {
	configPath := filepath.Join(dir, configFileName)
	options := onkcompile.CompileOptions{}
	if _, statErr := os.Stat(configPath); statErr == nil {
		cfg, configErr := LoadConfig(dir)
		if configErr != nil {
			return configErr
		}
		options.AllowLegacyContracts = cfg.AllowLegacyContracts
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat %s: %w", configPath, statErr)
	}

	_, err := CompileWithOptions(dir, options)
	return err
}

// Compatibility compares two schema directories and returns breaking changes.
func Compatibility(previousDir, currentDir string) ([]onkcompat.Finding, error) {
	previous, err := compileCompatibilityProject(previousDir)
	if err != nil {
		return nil, fmt.Errorf("compile previous schema: %w", err)
	}
	current, err := compileCompatibilityProject(currentDir)
	if err != nil {
		return nil, fmt.Errorf("compile current schema: %w", err)
	}
	return onkcompat.Compare(previous, current), nil
}

func compileCompatibilityProject(dir string) (*onkir.Package, error) {
	pkg, err := Compile(dir)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(dir, configFileName)
	if _, statErr := os.Stat(configPath); statErr == nil {
		cfg, loadErr := LoadConfig(dir)
		if loadErr != nil {
			return nil, loadErr
		}
		applyRoutePrefix(pkg, cfg.RoutePrefix)
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("stat %s: %w", configPath, statErr)
	}
	return pkg, nil
}

const (
	genDirPerm  = 0o755
	genFilePerm = 0o644
)

func writeFile(path string, data []byte) error {
	if len(data) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale generated output %s: %w", path, err)
		}
		return nil
	}
	err := os.MkdirAll(filepath.Dir(path), genDirPerm)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".onek-*")
	if err != nil {
		return fmt.Errorf("create temporary output for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(genFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set permissions on temporary output for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary output for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func lastPathSegment(p string) string {
	p = strings.TrimSuffix(p, "/")
	parts := strings.Split(filepath.ToSlash(p), "/")
	return parts[len(parts)-1]
}

func groupOutDir(outRoot, relDir string) string {
	return filepath.Join(outRoot, filepath.FromSlash(relDir))
}

// Build parses and compiles every .onk file under dir, then generates every
// target configured in onekit.toml.
func Build(dir string) error {
	cfg, err := LoadConfig(dir)
	if err != nil {
		return err
	}

	pkg, err := CompileWithOptions(dir, onkcompile.CompileOptions{
		AllowLegacyContracts: cfg.AllowLegacyContracts,
	})
	if err != nil {
		return err
	}
	applyRoutePrefix(pkg, cfg.RoutePrefix)
	idx, err := groupByDirectory(pkg, dir)
	if err != nil {
		return err
	}

	steps := []struct {
		enabled bool
		run     func() error
	}{
		{cfg.Generate.GoServer != nil || cfg.Generate.GoClient != nil, func() error { return buildGo(cfg, idx) }},
		{cfg.Generate.TSClient != nil, func() error { return buildTSClient(cfg, idx) }},
		{cfg.Generate.TSServer != nil, func() error { return buildTSServer(cfg, idx) }},
		{cfg.Generate.PythonClient != nil, func() error { return buildPythonClient(cfg, idx) }},
		{cfg.Generate.RustClient != nil || cfg.Generate.RustServer != nil, func() error { return buildRust(cfg, idx) }},
		{cfg.Generate.OpenAPI != nil, func() error { return buildOpenAPI(cfg, pkg) }},
	}
	for _, step := range steps {
		if !step.enabled {
			continue
		}
		err = step.run()
		if err != nil {
			return err
		}
	}
	if err := cleanupStaleGeneratedOutputs(cfg, idx); err != nil {
		return err
	}
	return writeGenerationManifest(cfg, idx)
}

type generationManifest struct {
	Version     int                 `json:"version"`
	Module      string              `json:"module"`
	RoutePrefix string              `json:"route_prefix,omitempty"`
	SchemaHash  string              `json:"schema_hash"`
	SchemaFiles []string            `json:"schema_files"`
	Outputs     map[string][]string `json:"outputs"`
}

// writeGenerationManifest records the exact schema/config fingerprint and
// generated output set. It gives CI and editor tooling a stable way to detect
// drift without guessing which files belong to OneKit.
func writeGenerationManifest(cfg *Config, idx *sourceIndex) error {
	paths, err := discoverOnkFiles(cfg.dir)
	if err != nil {
		return err
	}
	sort.Strings(paths)
	hash := sha256.New()
	var schemaFiles []string
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read schema for manifest %s: %w", path, readErr)
		}
		rel, relErr := filepath.Rel(cfg.dir, path)
		if relErr != nil {
			return fmt.Errorf("relativize schema %s: %w", path, relErr)
		}
		rel = filepath.ToSlash(rel)
		schemaFiles = append(schemaFiles, rel)
		_, _ = hash.Write([]byte(rel))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	configData, err := os.ReadFile(filepath.Join(cfg.dir, configFileName))
	if err != nil {
		return fmt.Errorf("read config for manifest: %w", err)
	}
	_, _ = hash.Write([]byte(configFileName))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(configData)

	outputs := map[string][]string{}
	for root, expected := range expectedGeneratedOutputs(cfg, idx) {
		rootRel, relErr := filepath.Rel(cfg.dir, root)
		if relErr != nil {
			return fmt.Errorf("relativize generated root %s: %w", root, relErr)
		}
		rootRel = filepath.ToSlash(rootRel)
		files := make([]string, 0, len(expected))
		for file := range expected {
			files = append(files, filepath.ToSlash(filepath.Join(rootRel, file)))
		}
		sort.Strings(files)
		outputs[rootRel] = files
	}
	manifest := generationManifest{
		Version:     1,
		Module:      cfg.Module,
		RoutePrefix: cfg.RoutePrefix,
		SchemaHash:  hex.EncodeToString(hash.Sum(nil)),
		SchemaFiles: schemaFiles,
		Outputs:     outputs,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generation manifest: %w", err)
	}
	data = append(data, '\n')
	return writeFile(filepath.Join(cfg.dir, ".onekit", "manifest.json"), data)
}

func cleanupStaleGeneratedOutputs(cfg *Config, idx *sourceIndex) error {
	expectedByRoot := expectedGeneratedOutputs(cfg, idx)
	roots := make([]string, 0, len(expectedByRoot))
	for root := range expectedByRoot {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		protected := make([]string, 0)
		for _, other := range roots {
			if root != other && pathWithin(root, other) {
				protected = append(protected, other)
			}
		}
		if err := cleanupGeneratedRoot(root, expectedByRoot[root], protected); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocognit // Target combinations intentionally share one explicit output manifest.
func expectedGeneratedOutputs(cfg *Config, idx *sourceIndex) map[string]map[string]bool {
	roots := map[string]map[string]bool{}
	add := func(root, rel string) {
		root = filepath.Clean(root)
		if roots[root] == nil {
			roots[root] = map[string]bool{}
		}
		roots[root][filepath.Clean(rel)] = true
	}
	for _, group := range idx.groups {
		rel := filepath.FromSlash(group.relDir)
		if rel == "." {
			rel = ""
		}
		if cfg.Generate.GoServer != nil || cfg.Generate.GoClient != nil {
			target := cfg.Generate.GoServer
			if target == nil {
				target = cfg.Generate.GoClient
			}
			root := cfg.resolve(target.Out)
			add(root, filepath.Join(rel, "types.gen.go"))
			add(root, filepath.Join(rel, "validate.gen.go"))
			if cfg.Generate.GoServer != nil {
				add(root, filepath.Join(rel, "server.gen.go"))
			}
			if cfg.Generate.GoClient != nil {
				add(root, filepath.Join(rel, "client.gen.go"))
			}
		}
		if cfg.Generate.TSClient != nil {
			root := cfg.resolve(cfg.Generate.TSClient.Out)
			add(root, filepath.Join(rel, "types.ts"))
			add(root, filepath.Join(rel, "client.ts"))
		}
		if cfg.Generate.TSServer != nil {
			root := cfg.resolve(cfg.Generate.TSServer.Out)
			add(root, filepath.Join(rel, "types.ts"))
			add(root, filepath.Join(rel, "server.ts"))
		}
		if cfg.Generate.PythonClient != nil {
			root := cfg.resolve(cfg.Generate.PythonClient.Out)
			add(root, "__init__.py")
			add(root, filepath.Join(rel, "models.py"))
			add(root, filepath.Join(rel, "client.py"))
			for parent := rel; parent != "." && parent != ""; parent = filepath.Dir(parent) {
				add(root, filepath.Join(parent, "__init__.py"))
			}
		}
	}
	addRustExpected := func(target *TargetConfig, client, server bool) {
		if target == nil {
			return
		}
		root := cfg.resolve(target.Out)
		add(root, "mod.rs")
		for _, group := range idx.groups {
			rel := filepath.FromSlash(group.relDir)
			if rel == "." {
				rel = ""
			}
			add(root, filepath.Join(rel, "types.rs"))
			if client {
				add(root, filepath.Join(rel, "client.rs"))
			}
			if server {
				add(root, filepath.Join(rel, "server.rs"))
			}
			for parent := filepath.Dir(rel); parent != "." && parent != ""; parent = filepath.Dir(parent) {
				add(root, filepath.Join(parent, "mod.rs"))
			}
			if rel != "" {
				add(root, filepath.Join(rel, "mod.rs"))
			}
		}
	}
	if cfg.Generate.RustClient != nil && cfg.Generate.RustServer != nil &&
		filepath.Clean(cfg.resolve(cfg.Generate.RustClient.Out)) == filepath.Clean(cfg.resolve(cfg.Generate.RustServer.Out)) {
		addRustExpected(cfg.Generate.RustClient, true, true)
	} else {
		addRustExpected(cfg.Generate.RustClient, true, false)
		addRustExpected(cfg.Generate.RustServer, false, true)
	}
	if cfg.Generate.OpenAPI != nil {
		add(cfg.resolve(cfg.Generate.OpenAPI.Out), "openapi.yaml")
	}
	return roots
}

func cleanupGeneratedRoot(root string, expected map[string]bool, protectedRoots []string) error {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat generated output root %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("generated output root %s is not a directory", root)
	}
	return filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			for _, protectedRoot := range protectedRoots {
				if filepath.Clean(filePath) == filepath.Clean(protectedRoot) {
					return fs.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, filePath)
		if relErr != nil || expected[filepath.Clean(rel)] || !isOnekitGeneratedFile(filePath) {
			return relErr
		}
		// #nosec G122 -- WalkDir does not follow directory symlinks, and only files with OneKit's generated banner are removed.
		if removeErr := os.Remove(filePath); removeErr != nil {
			return fmt.Errorf("remove stale generated output %s: %w", filePath, removeErr)
		}
		return nil
	})
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isOnekitGeneratedFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, 128)
	count, _ := file.Read(buffer)
	return strings.Contains(string(buffer[:count]), "Code generated by onek. DO NOT EDIT.")
}

// goPackageAlias derives a valid, collision-resistant Go import alias from a
// schema directory path (e.g. "common/pagination/v1" -> "common_pagination_v1").
func goPackageAlias(relDir string) string {
	if relDir == "." || relDir == "" {
		return "root"
	}
	segments := strings.Split(filepath.ToSlash(relDir), "/")
	for i, seg := range segments {
		segments[i] = strings.ReplaceAll(seg, "-", "_")
	}
	alias := strings.Join(segments, "_")
	if alias == "" {
		return "root"
	}
	if alias[0] >= '0' && alias[0] <= '9' {
		alias = "pkg_" + alias
	}
	return alias
}

func goImportPath(module, relDir string) string {
	if module == "" || relDir == "." || relDir == "" {
		return module
	}
	return module + "/" + relDir
}

// goResolver implements gengo.PackageResolver by looking up which schema
// directory produced a given message/enum, and treating anything outside the
// group currently being generated as external.
type goResolver struct {
	currentDir string
	idx        *sourceIndex
	packages   map[string]gengo.PackageRef
}

func (r *goResolver) resolve(dir string, ok bool) (gengo.PackageRef, bool) {
	if !ok || dir == r.currentDir {
		return gengo.PackageRef{}, false
	}
	ref, ok := r.packages[dir]
	return ref, ok
}

func (r *goResolver) ResolveMessage(m *onkir.Message) (gengo.PackageRef, bool) {
	dir, ok := r.idx.dirByMessage[m]
	return r.resolve(dir, ok)
}

func (r *goResolver) ResolveEnum(e *onkir.Enum) (gengo.PackageRef, bool) {
	dir, ok := r.idx.dirByEnum[e]
	return r.resolve(dir, ok)
}

func buildGoPackageRefs(module string, groups []*sourceGroup) map[string]gengo.PackageRef {
	refs := make(map[string]gengo.PackageRef, len(groups))
	for _, g := range groups {
		refs[g.relDir] = gengo.PackageRef{
			Alias:      goPackageAlias(g.relDir),
			ImportPath: goImportPath(module, g.relDir),
		}
	}
	return refs
}

func buildGo(cfg *Config, idx *sourceIndex) error {
	out := cfg.Generate.GoServer
	if out == nil {
		out = cfg.Generate.GoClient
	}
	typesOutRoot := cfg.resolve(out.Out)
	goRefs := buildGoPackageRefs(cfg.Module, idx.groups)

	for _, g := range idx.groups {
		outDir := groupOutDir(typesOutRoot, g.relDir)
		g.file.Package = lastPathSegment(outDir)
		resolver := &goResolver{currentDir: g.relDir, idx: idx, packages: goRefs}

		if err := writeGoTypesAndValidation(g.file, outDir, resolver); err != nil {
			return err
		}
		if cfg.Generate.GoServer != nil {
			serverOutDir := groupOutDir(cfg.resolve(cfg.Generate.GoServer.Out), g.relDir)
			if err := writeGoServer(g.file, serverOutDir, resolver); err != nil {
				return err
			}
		}
		if cfg.Generate.GoClient != nil {
			clientOutDir := groupOutDir(cfg.resolve(cfg.Generate.GoClient.Out), g.relDir)
			if err := writeGoClient(g.file, clientOutDir, resolver); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeGoTypesAndValidation(merged *onkir.File, outDir string, resolver gengo.PackageResolver) error {
	types, err := gengo.GenerateTypesWithResolver(merged, resolver)
	if err != nil {
		return fmt.Errorf("generate go types: %w", err)
	}
	err = writeFile(filepath.Join(outDir, "types.gen.go"), types)
	if err != nil {
		return err
	}

	validation, err := gengo.GenerateValidation(merged)
	if err != nil {
		return fmt.Errorf("generate go validation: %w", err)
	}
	return writeFile(filepath.Join(outDir, "validate.gen.go"), validation)
}

func writeGoServer(merged *onkir.File, outDir string, resolver gengo.PackageResolver) error {
	server, err := gengo.GenerateServerWithResolver(merged, resolver)
	if err != nil {
		return fmt.Errorf("generate go server: %w", err)
	}
	return writeFile(filepath.Join(outDir, "server.gen.go"), server)
}

func writeGoClient(merged *onkir.File, outDir string, resolver gengo.PackageResolver) error {
	client, err := gengo.GenerateClientWithResolver(merged, resolver)
	if err != nil {
		return fmt.Errorf("generate go client: %w", err)
	}
	return writeFile(filepath.Join(outDir, "client.gen.go"), client)
}

// buildTSClient, buildTSServer, and buildPythonClient generate one package
// per schema directory, mirroring the Go build's output layout, base_path
// inference, and cross-directory import resolution.
func buildTSClient(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.TSClient.Out)
	for _, g := range idx.groups {
		outDir := groupOutDir(outRoot, g.relDir)
		resolver := &tsResolver{currentDir: g.relDir, idx: idx}
		err := writeFile(filepath.Join(outDir, "types.ts"), gents.GenerateTypesWithResolver(g.file, resolver))
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(outDir, "client.ts"), gents.GenerateClientWithResolver(g.file, resolver))
		if err != nil {
			return err
		}
	}
	return nil
}

func buildTSServer(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.TSServer.Out)
	for _, g := range idx.groups {
		outDir := groupOutDir(outRoot, g.relDir)
		resolver := &tsResolver{currentDir: g.relDir, idx: idx}
		err := writeFile(filepath.Join(outDir, "types.ts"), gents.GenerateTypesWithResolver(g.file, resolver))
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(outDir, "server.ts"), gents.GenerateServerWithResolver(g.file, resolver))
		if err != nil {
			return err
		}
	}
	return nil
}

func buildPythonClient(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.PythonClient.Out)
	for _, g := range idx.groups {
		outDir := groupOutDir(outRoot, g.relDir)
		if err := writePythonInitFiles(outRoot, g.relDir); err != nil {
			return err
		}
		resolver := &pyResolver{currentDir: g.relDir, idx: idx}
		err := writeFile(filepath.Join(outDir, "models.py"), genpy.GenerateTypesWithResolver(g.file, resolver))
		if err != nil {
			return err
		}
		typesModule := pyModulePath(g.relDir)
		clientSrc := genpy.GenerateClientWithResolver(g.file, typesModule, resolver)
		err = writeFile(filepath.Join(outDir, "client.py"), clientSrc)
		if err != nil {
			return err
		}
	}
	return nil
}

type rustTarget struct {
	outRoot string
	client  bool
	server  bool
}

func buildRust(cfg *Config, idx *sourceIndex) error {
	targets := map[string]*rustTarget{}
	addTarget := func(target *TargetConfig, client, server bool) {
		if target == nil {
			return
		}
		outRoot := cfg.resolve(target.Out)
		entry, ok := targets[outRoot]
		if !ok {
			entry = &rustTarget{outRoot: outRoot}
			targets[outRoot] = entry
		}
		entry.client = entry.client || client
		entry.server = entry.server || server
	}
	addTarget(cfg.Generate.RustClient, true, false)
	addTarget(cfg.Generate.RustServer, false, true)

	orderedRoots := make([]string, 0, len(targets))
	for root := range targets {
		orderedRoots = append(orderedRoots, root)
	}
	sort.Strings(orderedRoots)
	for _, root := range orderedRoots {
		target := targets[root]
		for _, group := range idx.groups {
			outDir := groupOutDir(target.outRoot, group.relDir)
			resolver := &rustResolver{currentDir: group.relDir, idx: idx}
			if err := writeFile(filepath.Join(outDir, "types.rs"), genrust.GenerateTypesWithResolver(group.file, resolver)); err != nil {
				return err
			}
			if target.client {
				if err := writeFile(filepath.Join(outDir, "client.rs"), genrust.GenerateClientWithResolver(group.file, resolver)); err != nil {
					return err
				}
			}
			if target.server {
				if err := writeFile(filepath.Join(outDir, "server.rs"), genrust.GenerateServerWithResolver(group.file, resolver)); err != nil {
					return err
				}
			}
		}
		if err := writeRustModuleFiles(target.outRoot, idx.groups, target.client, target.server); err != nil {
			return err
		}
	}
	return nil
}

type rustModuleNode struct {
	hasTypes  bool
	hasClient bool
	hasServer bool
	children  map[string]bool
}

func writeRustModuleFiles(outRoot string, groups []*sourceGroup, client, server bool) error {
	nodes := map[string]*rustModuleNode{}
	nodeAt := func(path string) *rustModuleNode {
		node, ok := nodes[path]
		if !ok {
			node = &rustModuleNode{children: map[string]bool{}}
			nodes[path] = node
		}
		return node
	}
	nodeAt(".")
	for _, group := range groups {
		segments := rustPathSegments(group.relDir)
		parent := "."
		for _, segment := range segments {
			nodeAt(parent).children[segment] = true
			if parent == "." {
				parent = segment
			} else {
				parent += "/" + segment
			}
			nodeAt(parent)
		}
		leaf := nodeAt(parent)
		leaf.hasTypes = true
		leaf.hasClient = client && len(group.file.Services) > 0
		leaf.hasServer = server && len(group.file.Services) > 0
	}

	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		node := nodes[path]
		var source strings.Builder
		source.WriteString("// Code generated by onek. DO NOT EDIT.\n")
		if node.hasTypes {
			source.WriteString("pub mod types;\n")
		}
		if node.hasClient {
			source.WriteString("pub mod client;\n")
		}
		if node.hasServer {
			source.WriteString("pub mod server;\n")
		}
		children := make([]string, 0, len(node.children))
		for child := range node.children {
			children = append(children, child)
		}
		sort.Strings(children)
		for _, child := range children {
			ident := genrust.RustIdent(child)
			if ident != child {
				_, _ = fmt.Fprintf(&source, "#[path = %q]\n", child+"/mod.rs")
			}
			source.WriteString("pub mod " + ident + ";\n")
		}
		dir := outRoot
		if path != "." {
			dir = filepath.Join(outRoot, filepath.FromSlash(path))
		}
		if err := writeFile(filepath.Join(dir, "mod.rs"), []byte(source.String())); err != nil {
			return err
		}
	}
	return nil
}

func rustPathSegments(relDir string) []string {
	if relDir == "." || relDir == "" {
		return nil
	}
	return strings.Split(filepath.ToSlash(relDir), "/")
}

// buildOpenAPI stays a single combined document across the whole schema
// tree - one API surface, not one document per service - so it keeps using
// mergeFiles instead of the per-directory sourceIndex.
func buildOpenAPI(cfg *Config, pkg *onkir.Package) error {
	outDir := cfg.resolve(cfg.Generate.OpenAPI.Out)
	merged := mergeFiles(pkg, "")
	opts := genopenapi.Options{
		Title:       cfg.Generate.OpenAPI.Title,
		Version:     cfg.Generate.OpenAPI.Version,
		Description: cfg.Generate.OpenAPI.Description,
	}
	data, err := genopenapi.Generate(merged, opts)
	if err != nil {
		return fmt.Errorf("generate openapi: %w", err)
	}
	return writeFile(filepath.Join(outDir, "openapi.yaml"), data)
}
