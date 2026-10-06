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
)

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

type expectedOutputs map[string]map[string]bool

func (e expectedOutputs) add(root, rel string) {
	root = filepath.Clean(root)
	if e[root] == nil {
		e[root] = map[string]bool{}
	}
	e[root][filepath.Clean(rel)] = true
}

func expectedGeneratedOutputs(cfg *Config, idx *sourceIndex) map[string]map[string]bool {
	roots := expectedOutputs{}
	for _, group := range idx.groups {
		rel := filepath.FromSlash(group.relDir)
		if rel == "." {
			rel = ""
		}
		roots.addGoOutputs(cfg, rel)
		roots.addTSOutputs(cfg, rel)
		roots.addDartOutputs(cfg, idx, group, rel)
		roots.addSwiftOutputs(cfg, group, rel)
		roots.addPythonOutputs(cfg, rel)
	}
	if cfg.Generate.RustClient != nil && cfg.Generate.RustServer != nil &&
		filepath.Clean(cfg.resolve(cfg.Generate.RustClient.Out)) == filepath.Clean(cfg.resolve(cfg.Generate.RustServer.Out)) {
		roots.addRustOutputs(cfg, idx, cfg.Generate.RustClient, &cfg.Generate.RustClient.ServiceFilter, &cfg.Generate.RustServer.ServiceFilter)
	} else {
		roots.addRustOutputs(cfg, idx, cfg.Generate.RustClient, rustFilterOf(cfg.Generate.RustClient), nil)
		roots.addRustOutputs(cfg, idx, cfg.Generate.RustServer, nil, rustFilterOf(cfg.Generate.RustServer))
	}
	roots.addOpenAPIOutputs(cfg, idx)
	if dir, ok := cfg.Generate.GoServer.sharedRuntimeDir(); ok {
		roots.add(cfg.resolve(cfg.Generate.GoServer.Out), filepath.Join(filepath.FromSlash(dir), "runtime.gen.go"))
	}
	return roots
}

func (e expectedOutputs) addGoOutputs(cfg *Config, rel string) {
	if cfg.Generate.GoServer == nil && cfg.Generate.GoClient == nil {
		return
	}
	var outPath string
	if cfg.Generate.GoServer != nil {
		outPath = cfg.Generate.GoServer.Out
	} else {
		outPath = cfg.Generate.GoClient.Out
	}
	root := cfg.resolve(outPath)
	e.add(root, filepath.Join(rel, "types.gen.go"))
	e.add(root, filepath.Join(rel, "validate.gen.go"))
	if cfg.Generate.GoServer != nil {
		e.add(root, filepath.Join(rel, "server.gen.go"))
	}
	if cfg.Generate.GoClient != nil {
		e.add(root, filepath.Join(rel, "client.gen.go"))
	}
}

func (e expectedOutputs) addTSOutputs(cfg *Config, rel string) {
	if cfg.Generate.TSClient != nil {
		root := cfg.resolve(cfg.Generate.TSClient.Out)
		e.add(root, filepath.Join(rel, "types.ts"))
		e.add(root, filepath.Join(rel, "client.ts"))
		if cfg.Generate.TSClient.MSW {
			e.add(root, filepath.Join(rel, "msw.ts"))
		}
	}
	if cfg.Generate.TSServer != nil {
		root := cfg.resolve(cfg.Generate.TSServer.Out)
		e.add(root, filepath.Join(rel, "types.ts"))
		e.add(root, filepath.Join(rel, "server.ts"))
	}
}

func (e expectedOutputs) addDartOutputs(cfg *Config, idx *sourceIndex, group *sourceGroup, rel string) {
	if cfg.Generate.DartClient == nil {
		return
	}
	root := cfg.resolve(cfg.Generate.DartClient.Out)
	e.add(root, "onekit.dart")
	if idx.hasWS() {
		e.add(root, "onekit_ws.dart")
		e.add(root, "onekit_ws_io.dart")
		e.add(root, "onekit_ws_web.dart")
	}
	e.add(root, filepath.Join(rel, "models.dart"))
	if len(cfg.Generate.DartClient.apply(group.file).Services) > 0 {
		e.add(root, filepath.Join(rel, "client.dart"))
	}
}

func (e expectedOutputs) addSwiftOutputs(cfg *Config, group *sourceGroup, rel string) {
	if cfg.Generate.SwiftClient == nil {
		return
	}
	root := cfg.resolve(cfg.Generate.SwiftClient.Out)
	e.add(root, "Onekit.swift")
	models, client := swiftFileNames(filepath.ToSlash(rel))
	e.add(root, filepath.Join(rel, models))
	if swiftHasClient(cfg.Generate.SwiftClient.apply(group.file)) {
		e.add(root, filepath.Join(rel, client))
	}
}

func (e expectedOutputs) addPythonOutputs(cfg *Config, rel string) {
	if cfg.Generate.PythonClient == nil {
		return
	}
	root := cfg.resolve(cfg.Generate.PythonClient.Out)
	pyRel := filepath.FromSlash(pythonRelDir(filepath.ToSlash(rel)))
	e.add(root, "__init__.py")
	e.add(root, filepath.Join(pyRel, "models.py"))
	e.add(root, filepath.Join(pyRel, "client.py"))
	for parent := pyRel; parent != "." && parent != ""; parent = filepath.Dir(parent) {
		e.add(root, filepath.Join(parent, "__init__.py"))
	}
}

func rustFilterOf(target *TargetConfig) *ServiceFilter {
	if target == nil {
		return nil
	}
	return &target.ServiceFilter
}

func (e expectedOutputs) addRustOutputs(cfg *Config, idx *sourceIndex, target *TargetConfig, client, server *ServiceFilter) {
	if target == nil {
		return
	}
	root := cfg.resolve(target.Out)
	e.add(root, "mod.rs")
	for _, group := range idx.groups {
		rel := filepath.FromSlash(group.relDir)
		if rel == "." {
			rel = ""
		}
		e.add(root, filepath.Join(rel, "types.rs"))
		if client != nil && len(client.apply(group.file).Services) > 0 {
			e.add(root, filepath.Join(rel, "client.rs"))
		}
		if server != nil && len(server.apply(group.file).Services) > 0 {
			e.add(root, filepath.Join(rel, "server.rs"))
		}
		for parent := filepath.Dir(rel); parent != "." && parent != ""; parent = filepath.Dir(parent) {
			e.add(root, filepath.Join(parent, "mod.rs"))
		}
		if rel != "" {
			e.add(root, filepath.Join(rel, "mod.rs"))
		}
	}
}

func (e expectedOutputs) addOpenAPIOutputs(cfg *Config, idx *sourceIndex) {
	if cfg.Generate.OpenAPI == nil {
		return
	}
	root := filepath.Clean(cfg.resolve(cfg.Generate.OpenAPI.Out))
	if e[root] == nil {
		e[root] = map[string]bool{}
	}
	for _, full := range idx.groups {
		group := filteredGroup(full, cfg.Generate.OpenAPI.ServiceFilter)
		for _, service := range group.file.Services {
			base := openAPIBasePath(group, service)
			e.add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".yaml")
			e.add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".json")
		}
	}
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
