package onek

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1homsi/onekit/internal/onkmigrate"
)

// MigrateOptions controls `onkt migrate`.
type MigrateOptions struct {
	// KeepSchemas leaves the .onk files in place after writing the .tsp files.
	KeepSchemas bool
	// Force overwrites existing .tsp files.
	Force bool
}

// MigrateResult lists what a migration wrote and removed.
type MigrateResult struct {
	Written []string
	Removed []string
}

// toolVersion is the running onkt release ("dev" for a development build);
// projects onkt creates pin the matching @onkt/typespec.
var toolVersion = "dev"

// SetToolVersion records the running onkt release.
func SetToolVersion(version string) {
	toolVersion = version
}

// typespecPackageJSON is written for a project onkt creates or migrates, so
// `npm install` brings in the TypeSpec toolchain. @onkt/typespec is
// published with every onkt release under the same version.
func typespecPackageJSON() string {
	emitter := "latest"
	if version := NormalizeVersion(toolVersion); validatePinnedVersion(version) == nil && version != "" {
		emitter = "^" + version
	}
	return `{
  "private": true,
  "devDependencies": {
    "@onkt/typespec": "` + emitter + `",
    "@typespec/compiler": "^1.17.0",
    "@typespec/http": "^1.17.0"
  }
}
`
}

// Migrate converts every .onk schema under the project's schema root to
// TypeSpec: one .tsp file next to each .onk file, a main.tsp importing them
// all, and a package.json when the project has none. It renames onkt.toml
// to onkt.toml, and removes the .onk files unless KeepSchemas is set.
func Migrate(dir string, options MigrateOptions) (MigrateResult, error) {
	var result MigrateResult
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return result, err
	}
	root := dir
	if cfg != nil {
		root = cfg.SchemaDir()
	}
	root, err = canonicalProjectDir(root)
	if err != nil {
		return result, err
	}
	outputs, sources, err := convertSchemas(root, onkSchemaExtensions(cfg))
	if err != nil {
		return result, err
	}
	entry, standalone := onkmigrate.Entry(root, outputs)
	if standalone {
		outputs = append(outputs, entry)
	} else {
		for i := range outputs {
			if outputs[i].Path == entry.Path {
				outputs[i] = entry
			}
		}
	}
	if !options.Force {
		for _, out := range outputs {
			if _, err := os.Stat(out.Path); err == nil {
				return result, fmt.Errorf("%s already exists; pass --force to overwrite it", out.Path)
			}
		}
	}
	for _, out := range outputs {
		if err := os.WriteFile(out.Path, []byte(out.Content), genFilePerm); err != nil {
			return result, err
		}
		result.Written = append(result.Written, out.Path)
	}
	project := dir
	packageJSON := filepath.Join(project, "package.json")
	if _, err := os.Stat(packageJSON); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(packageJSON, []byte(typespecPackageJSON()), genFilePerm); err != nil {
			return result, err
		}
		result.Written = append(result.Written, packageJSON)
	}
	legacy := filepath.Join(project, legacyConfigFileName)
	current := filepath.Join(project, configFileName)
	if _, err := os.Stat(current); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(legacy); err == nil {
			if err := os.Rename(legacy, current); err != nil {
				return result, err
			}
			result.Written = append(result.Written, current)
			result.Removed = append(result.Removed, legacy)
		}
	}
	if !options.KeepSchemas {
		for _, src := range sources {
			if err := os.Remove(src); err != nil {
				return result, err
			}
			result.Removed = append(result.Removed, src)
		}
	}
	return result, nil
}

// onkSchemaExtensions is the configured schema extensions without .tsp: the
// files a migration converts.
func onkSchemaExtensions(cfg *Config) []string {
	var exts []string
	for _, ext := range cfg.schemaExtensions() {
		if ext != tspExtension {
			exts = append(exts, ext)
		}
	}
	return exts
}

// convertSchemas converts the .onk schemas under root to TypeSpec and
// returns the files to write and the sources they replace.
func convertSchemas(root string, exts []string) ([]onkmigrate.Output, []string, error) {
	files, err := discoverSchemaFiles(root, exts)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no .onk files found under %s", root)
	}
	parsed, err := parseSources(files)
	if err != nil {
		return nil, nil, err
	}
	sources := make([]onkmigrate.Source, len(parsed))
	for i, src := range parsed {
		sources[i] = onkmigrate.Source{Path: src.Path, AST: src.AST}
	}
	outputs, err := onkmigrate.Convert(sources, onkmigrate.Options{
		BasePath: func(path string) string {
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return ""
			}
			return inferBasePath(rel)
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return outputs, files, nil
}

// DescribeMigration is the summary `onkt migrate` prints.
func DescribeMigration(result MigrateResult) string {
	var b strings.Builder
	b.WriteString("migrated to TypeSpec")
	for _, path := range result.Written {
		fmt.Fprintf(&b, "\n  wrote   %s", path)
	}
	for _, path := range result.Removed {
		fmt.Fprintf(&b, "\n  removed %s", path)
	}
	b.WriteString("\nnext: run `npm install` in the project, then `onkt build`")
	return b.String()
}
