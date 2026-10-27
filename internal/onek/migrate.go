package onek

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// Notes are steps the migration left to the user.
	Notes []string
}

// toolVersion is the running onkt release ("dev" for a development build);
// projects onkt creates pin the matching @onkt/typespec.
var toolVersion = "dev"

// SetToolVersion records the running onkt release.
func SetToolVersion(version string) {
	toolVersion = version
}

// typespecPackageJSON is written for a project onkt creates or migrates, so
// `npm install` brings in the TypeSpec toolchain and the onkt CLI itself.
// onkt and @onkt/typespec are published with every release under the same
// version.
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
    "@typespec/http": "^1.17.0",
    "onkt": "` + emitter + `"
  }
}
`
}

// Migrate converts every .onk schema under the project's schema root to
// TypeSpec: one .tsp file next to each .onk file, a main.tsp importing them
// all, and a package.json when the project has none. It renames onekit.toml
// to onkt.toml, moves a pinned version to the running release, and removes
// the .onk files unless KeepSchemas is set.
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
	} else if data, err := os.ReadFile(packageJSON); err == nil && !strings.Contains(string(data), `"`+typespecEmitter+`"`) {
		result.Notes = append(result.Notes, fmt.Sprintf("%s already exists; add the TypeSpec toolchain to it: npm install --save-dev @typespec/compiler @typespec/http %s onkt", packageJSON, typespecEmitter))
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
	if note, err := repinConfig(current); err != nil {
		return result, err
	} else if note != "" {
		result.Notes = append(result.Notes, note)
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

var (
	pinnedVersionLine = regexp.MustCompile(`(?m)^(\s*version\s*=\s*)"([^"]*)"`)
	tableHeader       = regexp.MustCompile(`(?m)^\s*\[`)
)

// repinConfig moves a version pinned in the project config to the running
// release: an older pin would hand every build to a release that only reads
// .onk schemas. A development build cannot be pinned, so it only says so.
func repinConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Only the top-level key: tables such as [generate.openapi] have their
	// own version.
	top := data
	if loc := tableHeader.FindIndex(data); loc != nil {
		top = data[:loc[0]]
	}
	match := pinnedVersionLine.FindSubmatch(top)
	if match == nil {
		return "", nil
	}
	pinned := string(match[2])
	running := NormalizeVersion(toolVersion)
	if !versionPattern.MatchString(running) {
		return fmt.Sprintf("%s pins version %q; pin a release that reads TypeSpec (0.38.0 or newer)", path, pinned), nil
	}
	if NormalizeVersion(pinned) == running {
		return "", nil
	}
	updated := append(pinnedVersionLine.ReplaceAll(top, []byte(`${1}"`+running+`"`)), data[len(top):]...)
	if err := os.WriteFile(path, updated, genFilePerm); err != nil { //nolint:gosec // the project's own config
		return "", err
	}
	return fmt.Sprintf("%s: version pin moved from %q to %q", path, pinned, running), nil
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
	for _, note := range result.Notes {
		fmt.Fprintf(&b, "\n  note    %s", note)
	}
	b.WriteString("\nnext: run `npm install` in the project, then `onkt build`")
	return b.String()
}
