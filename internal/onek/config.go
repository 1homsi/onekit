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
	Module      string         `toml:"module"`
	RoutePrefix string         `toml:"route_prefix"`
	Generate    GenerateConfig `toml:"generate"`

	dir string
}

const configFileName = "onekit.toml"

func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	err = toml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if routeErr := validateRoutePrefix(cfg.RoutePrefix); routeErr != nil {
		return nil, fmt.Errorf("parse %s: %w", path, routeErr)
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
	if path.Clean(prefix) != prefix || strings.ContainsAny(prefix, "?#%{}") {
		return errors.New("route_prefix must be a canonical literal URL path")
	}
	return nil
}

func (c *Config) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.dir, path)
}
