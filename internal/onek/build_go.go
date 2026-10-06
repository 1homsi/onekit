package onek

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/onkir"
)

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
