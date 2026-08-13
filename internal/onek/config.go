package onek

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type TargetConfig struct {
	Out string `toml:"out"`
}

type OpenAPITargetConfig struct {
	Out         string `toml:"out"`
	Title       string `toml:"title"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
}

type GenerateConfig struct {
	GoServer     *TargetConfig        `toml:"go-server"`
	GoClient     *TargetConfig        `toml:"go-client"`
	TSClient     *TargetConfig        `toml:"ts-client"`
	TSServer     *TargetConfig        `toml:"ts-server"`
	PythonClient *TargetConfig        `toml:"python-client"`
	RustClient   *TargetConfig        `toml:"rust-client"`
	RustServer   *TargetConfig        `toml:"rust-server"`
	OpenAPI      *OpenAPITargetConfig `toml:"openapi"`
}

type Config struct {
	Module               string         `toml:"module"`
	RoutePrefix          string         `toml:"route_prefix"`
	AllowLegacyContracts bool           `toml:"allow_legacy_contracts"`
	Generate             GenerateConfig `toml:"generate"`

	dir string
}

const configFileName = "onekit.toml"

// ConfigError gives editor integrations a concrete file location for TOML
// and project configuration failures while preserving the wrapped cause.
type ConfigError struct {
	Path string
	Err  error
}

func (e *ConfigError) Error() string { return fmt.Sprintf("parse %s: %v", e.Path, e.Err) }

func (e *ConfigError) Unwrap() error { return e.Err }

func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("read: %w", err)}
	}
	var cfg Config
	metadata, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("unknown configuration key(s): %s", strings.Join(keys, ", "))}
	}
	if cfg.Module == "" {
		return nil, &ConfigError{Path: path, Err: errors.New("module is required")}
	}
	if err := validateRoutePrefix(cfg.RoutePrefix); err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	if err := validateTargetPaths(&cfg); err != nil {
		return nil, &ConfigError{Path: path, Err: err}
	}
	cfg.dir = dir
	return &cfg, nil
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
	if path.Clean(prefix) != prefix || strings.ContainsAny(prefix, "?#%{}") {
		return errors.New("route_prefix must be a canonical literal URL path")
	}
	return nil
}

func validateTargetPaths(cfg *Config) error {
	targets := []*TargetConfig{
		cfg.Generate.GoServer, cfg.Generate.GoClient, cfg.Generate.TSClient, cfg.Generate.TSServer, cfg.Generate.PythonClient,
		cfg.Generate.RustClient, cfg.Generate.RustServer,
	}
	for _, target := range targets {
		if target != nil && strings.TrimSpace(target.Out) == "" {
			return errors.New("generator output path must not be empty")
		}
	}
	if cfg.Generate.OpenAPI != nil && strings.TrimSpace(cfg.Generate.OpenAPI.Out) == "" {
		return errors.New("generator output path must not be empty")
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
