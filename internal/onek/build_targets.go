package onek

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/gendart"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/genswift"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkir"
)

// buildTSClient, buildTSServer, and buildPythonClient generate one package
// per schema directory, mirroring the Go build's output layout, base_path
// inference, and cross-directory import resolution.
func buildTSClient(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.TSClient.Out)
	opts := gents.Options{WireFieldNames: cfg.Generate.TSClient.FieldNames == fieldNamesWire}
	return eachGroup(idx, func(g *sourceGroup) error {
		outDir := groupOutDir(outRoot, g.relDir)
		resolver := &tsResolver{currentDir: g.relDir, idx: idx}
		err := writeFile(filepath.Join(outDir, "types.ts"), gents.GenerateTypesWithOptions(g.file, resolver, opts))
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(outDir, "client.ts"), gents.GenerateClientWithOptions(cfg.Generate.TSClient.apply(g.file), resolver, opts))
		if err != nil {
			return err
		}
		if cfg.Generate.TSClient.MSW {
			if err := writeFile(filepath.Join(outDir, "msw.ts"), gents.GenerateMSWHandlersWithOptions(cfg.Generate.TSClient.apply(g.file), resolver, opts)); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildTSServer(cfg *Config, idx *sourceIndex) error {
	outRoot := cfg.resolve(cfg.Generate.TSServer.Out)
	opts := gents.Options{WireFieldNames: cfg.Generate.TSServer.FieldNames == fieldNamesWire}
	return eachGroup(idx, func(g *sourceGroup) error {
		outDir := groupOutDir(outRoot, g.relDir)
		resolver := &tsResolver{currentDir: g.relDir, idx: idx}
		err := writeFile(filepath.Join(outDir, "types.ts"), gents.GenerateTypesWithOptions(g.file, resolver, opts))
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(outDir, "server.ts"), gents.GenerateServerWithOptions(cfg.Generate.TSServer.apply(g.file), resolver, opts))
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
		clientSrc := genpy.GenerateClientWithResolver(cfg.Generate.PythonClient.apply(g.file), typesModule, resolver)
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
		if client := gendart.GenerateClientWithResolver(cfg.Generate.DartClient.apply(g.file), resolver, runtimeDir); client != nil {
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
		if client := genswift.GenerateClientInNamespace(cfg.Generate.SwiftClient.apply(g.file), namespace, resolver); client != nil {
			return writeFile(filepath.Join(outDir, clientName), client)
		}
		return nil
	})
}

type rustTarget struct {
	outRoot      string
	client       bool
	server       bool
	clientFilter ServiceFilter
	serverFilter ServiceFilter
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
		if client {
			entry.client, entry.clientFilter = true, target.ServiceFilter
		}
		if server {
			entry.server, entry.serverFilter = true, target.ServiceFilter
		}
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
				if err := writeFile(filepath.Join(outDir, "client.rs"), genrust.GenerateClientWithResolver(target.clientFilter.apply(group.file), resolver)); err != nil {
					return err
				}
			}
			if target.server {
				if err := writeFile(filepath.Join(outDir, "server.rs"), genrust.GenerateServerWithResolver(target.serverFilter.apply(group.file), resolver)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := writeRustModuleFiles(target.outRoot, idx.groups, rustSide{target.client, target.clientFilter}, rustSide{target.server, target.serverFilter}); err != nil {
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

type rustSide struct {
	enabled bool
	filter  ServiceFilter
}

func (s rustSide) hasServicesIn(group *sourceGroup) bool {
	return s.enabled && len(s.filter.apply(group.file).Services) > 0
}

func writeRustModuleFiles(outRoot string, groups []*sourceGroup, client, server rustSide) error {
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
		leaf.hasClient = client.hasServicesIn(group)
		leaf.hasServer = server.hasServicesIn(group)
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
