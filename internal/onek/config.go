package onek

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type TargetConfig struct {
	Out string `toml:"out"`
}

// TSClientTargetConfig extends the plain output target with opt-in frontend
// artifacts emitted next to types.ts/client.ts: zod schemas (schemas.ts),
// TanStack Query hooks + SSE stream hooks (query.ts), and Mock Service Worker
// handlers (msw.ts).
type TSClientTargetConfig struct {
	Out        string `toml:"out"`
	Zod        bool   `toml:"zod"`
	ReactQuery bool   `toml:"react_query"`
	MSW        bool   `toml:"msw"`
}

type OpenAPITargetConfig struct {
	Out         string   `toml:"out"`
	Title       string   `toml:"title"`
	Version     string   `toml:"version"`
	Description string   `toml:"description"`
	Servers     []string `toml:"servers"`
}

type GenerateConfig struct {
	GoServer     *TargetConfig         `toml:"go-server"`
	GoClient     *TargetConfig         `toml:"go-client"`
	TSClient     *TSClientTargetConfig `toml:"ts-client"`
	TSServer     *TargetConfig         `toml:"ts-server"`
	PythonClient *TargetConfig         `toml:"python-client"`
	RustClient   *TargetConfig         `toml:"rust-client"`
	RustServer   *TargetConfig         `toml:"rust-server"`
	OpenAPI      *OpenAPITargetConfig  `toml:"openapi"`
}

type Config struct {
	Module string `toml:"module"`
	// SchemaRoot points at the directory holding the .onk schema tree,
	// relative to the project directory (the one containing onekit.toml).
	// It lets repositories keep schemas in a subdirectory while generator
	// outputs stay anchored elsewhere. Defaults to "." - i.e. the schema
	// tree and the project directory are the same. Base-path inference and
	// cross-package import mirroring are computed relative to this root;
	// output containment keeps being validated against the project dir.
	SchemaRoot           string         `toml:"schema_root"`
	RoutePrefix          string         `toml:"route_prefix"`
	AllowLegacyContracts bool           `toml:"allow_legacy_contracts"`
	Generate             GenerateConfig `toml:"generate"`

	dir       string
	schemaDir string
}

// ProjectDir returns the absolute canonical directory containing
// onekit.toml. Generator outputs and the drift manifest anchor here.
func (c *Config) ProjectDir() string { return c.dir }

func (c *Config) EnabledTargets() []string {
	var targets []string
	for _, target := range []struct {
		name    string
		enabled bool
	}{
		{"go-server", c.Generate.GoServer != nil},
		{"go-client", c.Generate.GoClient != nil},
		{"ts-client", c.Generate.TSClient != nil},
		{"ts-server", c.Generate.TSServer != nil},
		{"python-client", c.Generate.PythonClient != nil},
		{"rust-client", c.Generate.RustClient != nil},
		{"rust-server", c.Generate.RustServer != nil},
		{"openapi", c.Generate.OpenAPI != nil},
	} {
		if target.enabled {
			targets = append(targets, target.name)
		}
	}
	return targets
}

// SchemaDir returns the absolute canonical root of the .onk schema tree.
// It defaults to ProjectDir when schema_root is not configured.
func (c *Config) SchemaDir() string {
	if c.schemaDir == "" {
		return c.dir
	}
	return c.schemaDir
}

const configFileName = "onekit.toml"

// ConfigError gives editor integrations a concrete file location for TOML
// and project configuration failures while preserving the wrapped cause.
type ConfigError struct {
	Path   string
	Line   int
	Column int
	Err    error
}

func (e *ConfigError) Error() string {
	if e.Line > 0 && !tomlErrorLine.MatchString(e.Err.Error()) {
		return fmt.Sprintf("parse %s:%d:%d: %v", e.Path, e.Line, e.Column, e.Err)
	}
	return fmt.Sprintf("parse %s: %v", e.Path, e.Err)
}

func (e *ConfigError) Unwrap() error { return e.Err }

func LoadConfig(dir string) (*Config, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, &ConfigError{Path: filepath.Join(dir, configFileName), Err: err}
	}
	path := filepath.Join(root, configFileName)
	data, err := readRegularFile(path)
	if err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("read: %w", err)}
	}
	var cfg Config
	metadata, err := toml.Decode(string(data), &cfg)
	if err != nil {
		configErr := &ConfigError{Path: path, Err: err}
		var parseErr toml.ParseError
		if errors.As(err, &parseErr) {
			configErr.Line, configErr.Column = parseErr.Position.Line, parseErr.Position.Col
		} else if match := tomlErrorLine.FindStringSubmatch(err.Error()); match != nil {
			configErr.Line, _ = strconv.Atoi(match[1])
			if lines := strings.Split(string(data), "\n"); configErr.Line <= len(lines) {
				line := lines[configErr.Line-1]
				configErr.Column = len(line) - len(strings.TrimLeft(line, " \t")) + 1
			}
		}
		return nil, configErr
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		line, column := tomlKeyPosition(string(data), undecoded[0])
		return nil, &ConfigError{Path: path, Line: line, Column: column, Err: fmt.Errorf("unknown configuration key(s): %s", strings.Join(describeUnknownKeys(keys), ", "))}
	}
	if cfg.Module == "" {
		return nil, &ConfigError{Path: path, Err: errors.New("module is required")}
	}
	if err := validateRoutePrefix(cfg.RoutePrefix); err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	cfg.dir = root
	if err := resolveSchemaRootConfig(&cfg); err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	if err := validateTargetPaths(&cfg); err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	return &cfg, nil
}

// resolveSchemaRootConfig validates and resolves Config.SchemaRoot against
// the project directory. Empty values default to the project dir itself.
func resolveSchemaRootConfig(cfg *Config) error {
	cfg.schemaDir = cfg.dir
	if cfg.SchemaRoot == "" {
		return nil
	}
	if filepath.IsAbs(cfg.SchemaRoot) {
		return errors.New("schema_root must be relative to the project directory")
	}
	clean := filepath.Clean(filepath.FromSlash(cfg.SchemaRoot))
	rel, err := filepath.Rel(cfg.dir, filepath.Join(cfg.dir, clean))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("schema_root %q must stay inside the project directory", cfg.SchemaRoot)
	}
	if rel == "." {
		return nil // explicit "." - schema tree is the project dir
	}
	resolved, err := canonicalProjectDir(filepath.Join(cfg.dir, clean))
	if err != nil {
		return fmt.Errorf("resolve schema_root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stat schema_root: %w", err)
	}
	if !info.IsDir() {
		return errors.New("schema_root must be a directory")
	}
	cfg.schemaDir = resolved
	return nil
}

func validateRoutePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if !strings.HasPrefix(prefix, "/") {
		return errors.New("route_prefix must start with /")
	}
	if prefix == "/" {
		return errors.New("route_prefix must not be /; omit it instead")
	}
	if strings.HasSuffix(prefix, "/") {
		return errors.New("route_prefix must not end with /")
	}
	if path.Clean(prefix) != prefix || strings.ContainsAny(prefix, "?#%{}\\\"\r\n\t") {
		return errors.New("route_prefix must be a canonical literal URL path")
	}
	// Restrict every path segment to RFC 3986 pchar-safe characters so the
	// prefix cannot smuggle spaces, control characters, or delimiters into
	// generated routes and OpenAPI server URLs.
	for _, segment := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
		for _, r := range segment {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case strings.ContainsRune("-._~!$&'()*+,;=:@", r):
			default:
				return fmt.Errorf("route_prefix contains character %q which is not allowed in a URL path; use only letters, digits, and -._~!$&'()*+,;=:@", r)
			}
		}
	}
	return nil
}

func validateTargetPaths(cfg *Config) error {
	targets := []*TargetConfig{
		cfg.Generate.GoServer, cfg.Generate.GoClient, cfg.Generate.TSServer, cfg.Generate.PythonClient,
		cfg.Generate.RustClient, cfg.Generate.RustServer,
	}
	for _, target := range targets {
		if target != nil {
			if strings.TrimSpace(target.Out) == "" {
				return errors.New("generator output path must not be empty")
			}
			if err := validateContainedOutput(cfg.dir, target.Out); err != nil {
				return err
			}
		}
	}
	if cfg.Generate.TSClient != nil {
		if strings.TrimSpace(cfg.Generate.TSClient.Out) == "" {
			return errors.New("generator output path must not be empty")
		}
		if err := validateContainedOutput(cfg.dir, cfg.Generate.TSClient.Out); err != nil {
			return err
		}
	}
	if cfg.Generate.OpenAPI != nil {
		if strings.TrimSpace(cfg.Generate.OpenAPI.Out) == "" {
			return errors.New("generator output path must not be empty")
		}
		if err := validateContainedOutput(cfg.dir, cfg.Generate.OpenAPI.Out); err != nil {
			return err
		}
	}
	if cfg.Generate.GoServer != nil && cfg.Generate.GoClient != nil &&
		filepath.Clean(cfg.Generate.GoServer.Out) != filepath.Clean(cfg.Generate.GoClient.Out) {
		return errors.New("go-server and go-client must use the same output path so they share generated types")
	}
	return nil
}

func (c *Config) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.dir, path)
}

var tomlErrorLine = regexp.MustCompile(`^toml: line (\d+)`)

func tomlKeyPosition(text string, key toml.Key) (int, int) {
	unquote := func(part string) string {
		return strings.Trim(strings.TrimSpace(part), `"'`)
	}
	splitKey := func(raw string) []string {
		parts := strings.Split(raw, ".")
		for i, part := range parts {
			parts[i] = unquote(part)
		}
		return parts
	}
	var table []string
	for index, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(trimmed, "["):
			header := strings.Trim(strings.SplitN(trimmed, "#", 2)[0], "[] \t")
			table = splitKey(header)
			if slices.Equal(table, key) {
				return index + 1, strings.Index(line, "[") + 1
			}
			continue
		}
		name, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		full := append(slices.Clone(table), splitKey(name)...)
		if slices.Equal(full, key) || len(full) < len(key) && slices.Equal(full, key[:len(full)]) {
			return index + 1, len(line) - len(strings.TrimLeft(line, " \t")) + 1
		}
	}
	return 0, 0
}
