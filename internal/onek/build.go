package onek

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/genopenapi"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/gents"
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
			return nil, fmt.Errorf("parse %s: %w", path, err)
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

// Check parses and compiles every .onk file under dir without generating
// any output, returning the first error encountered.
func Check(dir string) error {
	files, err := discoverOnkFiles(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .onk files found under %s", dir)
	}
	sources, err := parseSources(files)
	if err != nil {
		return err
	}
	_, err = onkcompile.Compile(sources)
	return err
}

const (
	genDirPerm  = 0o750
	genFilePerm = 0o600
)

func writeFile(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	err := os.MkdirAll(filepath.Dir(path), genDirPerm)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	err = os.WriteFile(path, data, genFilePerm)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
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

	files, err := discoverOnkFiles(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .onk files found under %s", dir)
	}
	sources, err := parseSources(files)
	if err != nil {
		return err
	}
	pkg, err := onkcompile.Compile(sources)
	if err != nil {
		return err
	}
	err = applyDefaultBasePaths(pkg, dir)
	if err != nil {
		return err
	}
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
	return nil
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
