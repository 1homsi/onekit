package onek

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/1homsi/onekit/internal/gendart"
	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/genswift"
	"github.com/1homsi/onekit/internal/gents"
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
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	files, err := discoverOnkFiles(root)
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
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return "", err
	}
	if cfg != nil {
		return cfg.SchemaDir(), nil
	}
	return dir, nil
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
	_, err := CompileWithOptions(root, cfg.CompileOptions())
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
	switch {
	case cfg != nil:
		root = cfg.SchemaDir()
		options = cfg.CompileOptions()
		routePrefix = cfg.RoutePrefix
	case fallback != nil:
		options = fallback.CompileOptions()
		routePrefix = fallback.RoutePrefix
		if fallback.SchemaRoot != "" {
			if info, statErr := os.Stat(filepath.Join(dir, fallback.SchemaRoot)); statErr == nil && info.IsDir() {
				root = filepath.Join(dir, fallback.SchemaRoot)
			}
		}
	}
	pkg, err := CompileWithOptions(root, options)
	if err != nil {
		return nil, err
	}
	applyRoutePrefix(pkg, routePrefix)
	return pkg, nil
}

const (
	genDirPerm  = 0o755
	genFilePerm = 0o644
)

func writeFile(path string, data []byte) error {
	if captured != nil {
		captureWriteMu.Lock()
		captured[path] = data
		captureWriteMu.Unlock()
		return nil
	}
	dir := filepath.Dir(path)
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	if len(data) == 0 {
		return removeStaleOutput(path)
	}
	if filepath.Base(dir) != ".onekit" {
		writtenFiles.Add(1)
	}
	if err := ensureOutputDir(dir); err != nil {
		return err
	}
	created, err := createNewOutput(path, data)
	if errors.Is(err, fs.ErrNotExist) {
		createdOutputDirs.Delete(dir)
		if err = ensureOutputDir(dir); err != nil {
			return err
		}
		created, err = createNewOutput(path, data)
	}
	if err != nil {
		return err
	}
	if created {
		recordOutput(path, data)
		return nil
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing symlink path component %s", path)
	case err == nil && unchangedOnDisk(path, info, data):
		recordOutput(path, data)
		return nil
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	return replaceOutput(path, dir, data)
}

func removeStaleOutput(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing symlink path component %s", path)
	case err != nil && os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect path %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale generated output %s: %w", path, err)
	}
	return nil
}

func ensureOutputDir(dir string) error {
	if _, ok := createdOutputDirs.Load(dir); ok {
		return nil
	}
	if err := os.MkdirAll(dir, genDirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	createdOutputDirs.Store(dir, struct{}{})
	return nil
}

func createNewOutput(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, genFilePerm)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	fail := func(step string, cause error) (bool, error) {
		_ = f.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("%s %s: %w", step, path, cause)
	}
	if err := ensureModeAfterCreate(f); err != nil {
		return fail("set permissions on", err)
	}
	if _, err := f.Write(data); err != nil {
		return fail("write", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("close %s: %w", path, err)
	}
	return true, nil
}

func ensureModeAfterCreate(f *os.File) error {
	if createModeTrusted.Load() {
		return nil
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() == genFilePerm {
		createModeTrusted.Store(true)
		return nil
	}
	return f.Chmod(genFilePerm)
}

func replaceOutput(path, dir string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".onek-*")
	if err != nil {
		return fmt.Errorf("create temporary output for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()
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
	renamed = true
	recordOutput(path, data)
	return nil
}

var (
	createdOutputDirs sync.Map
	createModeTrusted atomic.Bool
)

func eachGroup(idx *sourceIndex, fn func(*sourceGroup) error) error {
	errs := make([]error, len(idx.groups))
	limit := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, g := range idx.groups {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			errs[i] = fn(g)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func unchangedOnDisk(path string, info os.FileInfo, data []byte) bool {
	if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != genFilePerm {
		return false
	}
	existing, err := os.ReadFile(path)
	return err == nil && bytes.Equal(existing, data)
}

func lastPathSegment(p string) string {
	p = strings.TrimSuffix(p, "/")
	parts := strings.Split(filepath.ToSlash(p), "/")
	return parts[len(parts)-1]
}

func goPackageIdent(segment string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(segment) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func groupOutDir(outRoot, relDir string) string {
	return filepath.Join(outRoot, filepath.FromSlash(relDir))
}

type BuildSummary struct {
	Targets []string
	Files   int
	Cached  bool
}

func BuildWithSummary(dir string) (BuildSummary, error) {
	writtenFiles.Store(0)
	cached, err := build(dir)
	summary := BuildSummary{Files: int(writtenFiles.Load()), Cached: cached}
	if cfg, cfgErr := LoadConfig(dir); cfgErr == nil {
		summary.Targets = cfg.EnabledTargets()
	}
	return summary, err
}

var (
	writtenFiles   atomic.Int64
	captureWriteMu sync.Mutex
)

// Build parses and compiles every .onk file under dir, then generates every
// target configured in onekit.toml.
func Build(dir string) error {
	_, err := build(dir)
	return err
}

func build(dir string) (bool, error) {
	resetSymlinkCheckCache()
	cfg, err := LoadConfig(dir)
	if err != nil {
		return false, err
	}

	startFiles := writtenFiles.Load()
	fingerprint := ""
	if captured == nil {
		if sum, sumErr := buildFingerprint(cfg); sumErr == nil {
			fingerprint = sum
		}
	}
	if fingerprint != "" && tryCachedBuild(cfg, fingerprint) {
		return true, nil
	}
	recording := fingerprint != ""
	exeHash := make(chan string, 1)
	if recording {
		go func() {
			hash, _ := executableHash()
			exeHash <- hash
		}()
		startRecordingOutputs()
		defer func() {
			if recording {
				stopRecordingOutputs()
			}
		}()
	}

	pkg, err := CompileWithOptions(cfg.SchemaDir(), cfg.CompileOptions())
	if err != nil {
		return false, err
	}
	applyRoutePrefix(pkg, cfg.RoutePrefix)
	idx, err := groupByDirectory(pkg, cfg.SchemaDir())
	if err != nil {
		return false, err
	}

	steps := []struct {
		enabled bool
		run     func() error
	}{
		{cfg.Generate.GoServer != nil || cfg.Generate.GoClient != nil, func() error { return buildGo(cfg, idx) }},
		{cfg.Generate.TSClient != nil, func() error { return buildTSClient(cfg, idx) }},
		{cfg.Generate.TSServer != nil, func() error { return buildTSServer(cfg, idx) }},
		{cfg.Generate.PythonClient != nil, func() error { return buildPythonClient(cfg, idx) }},
		{cfg.Generate.DartClient != nil, func() error { return buildDartClient(cfg, idx) }},
		{cfg.Generate.SwiftClient != nil, func() error { return buildSwiftClient(cfg, idx) }},
		{cfg.Generate.RustClient != nil || cfg.Generate.RustServer != nil, func() error { return buildRust(cfg, idx) }},
		{cfg.Generate.OpenAPI != nil, func() error { return buildOpenAPI(cfg, idx) }},
	}
	for _, step := range steps {
		if !step.enabled {
			continue
		}
		err = step.run()
		if err != nil {
			return false, err
		}
	}
	if captured != nil {
		return false, nil
	}
	if err := cleanupStaleGeneratedOutputs(cfg, idx); err != nil {
		return false, err
	}
	if err := writeGenerationManifest(cfg, idx); err != nil {
		return false, err
	}
	if recording {
		digests := stopRecordingOutputs()
		recording = false
		saveBuildCache(cfg, fingerprint, <-exeHash, digests, int(writtenFiles.Load()-startFiles))
	}
	return false, nil
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
	paths, err := discoverOnkFiles(cfg.SchemaDir())
	if err != nil {
		return err
	}
	sort.Strings(paths)
	hash := sha256.New()
	var schemaFiles []string
	for _, path := range paths {
		data, readErr := readRegularFile(path)
		if readErr != nil {
			return fmt.Errorf("read schema for manifest %s: %w", path, readErr)
		}
		rel, relErr := filepath.Rel(cfg.SchemaDir(), path)
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
	configData, err := readRegularFile(filepath.Join(cfg.dir, configFileName))
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
	ownedByRoot, err := previousGeneratedOutputs(cfg)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(expectedByRoot)+len(ownedByRoot))
	for root := range expectedByRoot {
		roots = append(roots, root)
	}
	for root := range ownedByRoot {
		if _, current := expectedByRoot[root]; !current {
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	for _, root := range roots {
		protected := make([]string, 0)
		for _, other := range roots {
			if root != other && pathWithin(root, other) {
				protected = append(protected, other)
			}
		}
		if err := cleanupGeneratedRoot(root, expectedByRoot[root], ownedByRoot[root], protected); err != nil {
			return err
		}
	}
	return nil
}

func previousGeneratedOutputs(cfg *Config) (map[string]map[string]bool, error) {
	owned := map[string]map[string]bool{}
	manifestPath := filepath.Join(cfg.dir, ".onekit", "manifest.json")
	data, err := readRegularFile(manifestPath)
	if os.IsNotExist(err) {
		return owned, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read generation manifest for cleanup: %w", err)
	}
	var manifest generationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse generation manifest for cleanup: %w", err)
	}
	for rootRel, files := range manifest.Outputs {
		root := filepath.Clean(filepath.Join(cfg.dir, filepath.FromSlash(rootRel)))
		if !pathWithin(cfg.dir, root) {
			continue
		}
		for _, file := range files {
			rel, err := filepath.Rel(rootRel, filepath.FromSlash(file))
			if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if owned[root] == nil {
				owned[root] = map[string]bool{}
			}
			owned[root][filepath.Clean(rel)] = true
		}
	}
	return owned, nil
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
			if cfg.Generate.TSClient.MSW {
				add(root, filepath.Join(rel, "msw.ts"))
			}
		}
		if cfg.Generate.TSServer != nil {
			root := cfg.resolve(cfg.Generate.TSServer.Out)
			add(root, filepath.Join(rel, "types.ts"))
			add(root, filepath.Join(rel, "server.ts"))
		}
		if cfg.Generate.DartClient != nil {
			root := cfg.resolve(cfg.Generate.DartClient.Out)
			add(root, "onekit.dart")
			if idx.hasWS() {
				add(root, "onekit_ws.dart")
				add(root, "onekit_ws_io.dart")
				add(root, "onekit_ws_web.dart")
			}
			add(root, filepath.Join(rel, "models.dart"))
			if len(group.file.Services) > 0 {
				add(root, filepath.Join(rel, "client.dart"))
			}
		}
		if cfg.Generate.SwiftClient != nil {
			root := cfg.resolve(cfg.Generate.SwiftClient.Out)
			add(root, "Onekit.swift")
			models, client := swiftFileNames(filepath.ToSlash(rel))
			add(root, filepath.Join(rel, models))
			if swiftHasClient(group.file) {
				add(root, filepath.Join(rel, client))
			}
		}
		if cfg.Generate.PythonClient != nil {
			root := cfg.resolve(cfg.Generate.PythonClient.Out)
			pyRel := filepath.FromSlash(pythonRelDir(filepath.ToSlash(rel)))
			add(root, "__init__.py")
			add(root, filepath.Join(pyRel, "models.py"))
			add(root, filepath.Join(pyRel, "client.py"))
			for parent := pyRel; parent != "." && parent != ""; parent = filepath.Dir(parent) {
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
		root := filepath.Clean(cfg.resolve(cfg.Generate.OpenAPI.Out))
		if roots[root] == nil {
			roots[root] = map[string]bool{}
		}
		for _, group := range idx.groups {
			for _, service := range group.file.Services {
				base := openAPIBasePath(group, service)
				add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".yaml")
				add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".json")
			}
		}
	}
	return roots
}

func cleanupGeneratedRoot(root string, expected, owned map[string]bool, protectedRoots []string) error {
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
		if entry.Type()&os.ModeSymlink != 0 || entry.Type()&os.ModeNamedPipe != 0 ||
			entry.Type()&os.ModeDevice != 0 || entry.Type()&os.ModeSocket != 0 {
			return nil
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
		if relErr != nil || expected[filepath.Clean(rel)] || !owned[filepath.Clean(rel)] {
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

// goPackageAlias derives a valid, collision-resistant Go import alias from a
// schema directory path (e.g. "common/pagination/v1" -> "common_pagination_v1").
func goPackageAlias(relDir string) string {
	if relDir == "." || relDir == "" {
		return "root"
	}
	segments := strings.Split(filepath.ToSlash(relDir), "/")
	for i, seg := range segments {
		var safe strings.Builder
		for _, r := range seg {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				safe.WriteRune(r)
			} else {
				safe.WriteByte('_')
			}
		}
		segments[i] = safe.String()
	}
	alias := strings.Join(segments, "_")
	if alias == "" {
		return "root"
	}
	if alias[0] >= '0' && alias[0] <= '9' {
		alias = "pkg_" + alias
	}
	if goKeywords[alias] {
		alias = "pkg_" + alias
	}
	return alias
}

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
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

	return eachGroup(idx, func(g *sourceGroup) error {
		outDir := groupOutDir(typesOutRoot, g.relDir)
		g.file.Package = goPackageIdent(lastPathSegment(outDir))
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
		return nil
	})
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

	validation, err := gengo.GenerateValidationWithResolver(merged, resolver)
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
	return eachGroup(idx, func(g *sourceGroup) error {
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
		if cfg.Generate.TSClient.MSW {
			if err := writeFile(filepath.Join(outDir, "msw.ts"), gents.GenerateMSWHandlersWithResolver(g.file, resolver)); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildTSServer(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.TSServer.Out)
	return eachGroup(idx, func(g *sourceGroup) error {
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
		return nil
	})
}

func buildPythonClient(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.PythonClient.Out)
	err := eachGroup(idx, func(g *sourceGroup) error {
		pyDir := pythonRelDir(g.relDir)
		outDir := groupOutDir(outRoot, pyDir)
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
		return nil
	})
	if err != nil {
		return err
	}
	for _, g := range idx.groups {
		if err := writePythonInitFiles(outRoot, pythonRelDir(g.relDir)); err != nil {
			return err
		}
	}
	return nil
}

func buildDartClient(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.DartClient.Out)
	runtimeFiles := map[string][]byte{"onekit.dart": gendart.GenerateRuntime()}
	if idx.hasWS() {
		runtimeFiles["onekit_ws.dart"] = gendart.GenerateWSRuntime()
		runtimeFiles["onekit_ws_io.dart"] = gendart.GenerateWSConnectIO()
		runtimeFiles["onekit_ws_web.dart"] = gendart.GenerateWSConnectWeb()
	}
	for name, src := range runtimeFiles {
		if err := writeFile(filepath.Join(outRoot, name), src); err != nil {
			return err
		}
	}
	return eachGroup(idx, func(g *sourceGroup) error {
		outDir := groupOutDir(outRoot, g.relDir)
		resolver := &dartResolver{currentDir: g.relDir, idx: idx}
		runtimeDir := dartRuntimeDir(g.relDir)
		if err := writeFile(filepath.Join(outDir, "models.dart"), gendart.GenerateTypesWithResolver(g.file, resolver, runtimeDir+"onekit.dart")); err != nil {
			return err
		}
		if client := gendart.GenerateClientWithResolver(g.file, resolver, runtimeDir); client != nil {
			if err := writeFile(filepath.Join(outDir, "client.dart"), client); err != nil {
				return err
			}
		}
		return nil
	})
}

func swiftFileNames(relDir string) (string, string) {
	slug := strings.Trim(strings.ReplaceAll(filepath.ToSlash(relDir), "/", "__"), "._")
	if slug == "" {
		slug = "Root"
	}
	return slug + "__Models.swift", slug + "__Client.swift"
}

func swiftHasClient(file *onkir.File) bool {
	return genswift.GenerateClient(file) != nil
}

func checkSwiftNamesUnique(idx *sourceIndex) error {
	namespaces := map[string]string{}
	var rootNames map[string]bool
	for _, g := range idx.groups {
		label := g.relDir
		if label == "." || label == "" {
			label = "the schema root"
		}
		namespace := genswift.Namespace(g.relDir)
		if namespace != "" {
			if other, ok := namespaces[namespace]; ok {
				return fmt.Errorf("swift-client: %s and %s both map to the Swift namespace %q; rename one of the directories", other, label, namespace)
			}
			namespaces[namespace] = label
		}
		seen := map[string]bool{}
		for _, name := range genswift.DeclaredNames(g.file) {
			if seen[name] {
				return fmt.Errorf("swift-client: %q is declared more than once in %s; Swift needs every generated type name in a package to be unique", name, label)
			}
			seen[name] = true
		}
		if namespace == "" {
			rootNames = seen
		}
	}
	for namespace, label := range namespaces {
		if rootNames[namespace] {
			return fmt.Errorf("swift-client: the schema root declares %q, which is also the Swift namespace for %s; rename one of them", namespace, label)
		}
	}
	return nil
}

func buildSwiftClient(cfg *Config, idx *sourceIndex) error {
	if err := checkSwiftNamesUnique(idx); err != nil {
		return err
	}
	outRoot := cfg.resolve(cfg.Generate.SwiftClient.Out)
	if err := writeFile(filepath.Join(outRoot, "Onekit.swift"), genswift.GenerateRuntime()); err != nil {
		return err
	}
	return eachGroup(idx, func(g *sourceGroup) error {
		outDir := groupOutDir(outRoot, g.relDir)
		models, clientName := swiftFileNames(g.relDir)
		namespace := genswift.Namespace(g.relDir)
		resolver := &swiftResolver{currentDir: g.relDir, idx: idx}
		if err := writeFile(filepath.Join(outDir, models), genswift.GenerateTypesInNamespace(g.file, namespace, resolver)); err != nil {
			return err
		}
		if client := genswift.GenerateClientInNamespace(g.file, namespace, resolver); client != nil {
			return writeFile(filepath.Join(outDir, clientName), client)
		}
		return nil
	})
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
		err := eachGroup(idx, func(group *sourceGroup) error {
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
			return nil
		})
		if err != nil {
			return err
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
