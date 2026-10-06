package onek

import (
	"fmt"
	"path"
	"slices"

	"github.com/1homsi/onekit/internal/onkir"
)

// ServiceFilter narrows the services a generator target produces code for.
// Patterns are shell-style globs over the service name (Orders, Admin*).
// With include_services set, only matching services are generated; any
// service matching exclude_services is then dropped. Types are always
// generated in full, so a client and a server can keep sharing them.
type ServiceFilter struct {
	IncludeServices []string `toml:"include_services"`
	ExcludeServices []string `toml:"exclude_services"`
}

func (f ServiceFilter) active() bool {
	return len(f.IncludeServices) > 0 || len(f.ExcludeServices) > 0
}

func (f ServiceFilter) keeps(name string) bool {
	if len(f.IncludeServices) > 0 && !matchesAny(f.IncludeServices, name) {
		return false
	}
	return !matchesAny(f.ExcludeServices, name)
}

func matchesAny(patterns []string, name string) bool {
	return slices.ContainsFunc(patterns, func(pattern string) bool {
		matched, err := path.Match(pattern, name)
		return err == nil && matched
	})
}

// apply returns file with only the services the filter keeps. The original is
// never modified.
func (f ServiceFilter) apply(file *onkir.File) *onkir.File {
	if !f.active() || file == nil {
		return file
	}
	filtered := *file
	filtered.Services = nil
	for _, service := range file.Services {
		if f.keeps(service.Name) {
			filtered.Services = append(filtered.Services, service)
		}
	}
	return &filtered
}

func (f ServiceFilter) validate(target string) error {
	for _, field := range []struct {
		name     string
		patterns []string
	}{{"include_services", f.IncludeServices}, {"exclude_services", f.ExcludeServices}} {
		for _, pattern := range field.patterns {
			if _, err := path.Match(pattern, ""); err != nil {
				return fmt.Errorf("%s %s: invalid pattern %q: %w", target, field.name, pattern, err)
			}
		}
	}
	return nil
}

// checkMatches reports a pattern that matches no service anywhere in the
// project, which is nearly always a typo that would silently generate nothing.
func (f ServiceFilter) checkMatches(target string, idx *sourceIndex) error {
	for _, field := range []struct {
		name     string
		patterns []string
	}{{"include_services", f.IncludeServices}, {"exclude_services", f.ExcludeServices}} {
		for _, pattern := range field.patterns {
			found := false
			for _, group := range idx.groups {
				for _, service := range group.file.Services {
					if matched, _ := path.Match(pattern, service.Name); matched {
						found = true
					}
				}
			}
			if !found {
				return fmt.Errorf("%s %s pattern %q matches no service", target, field.name, pattern)
			}
		}
	}
	return nil
}

type namedFilter struct {
	target string
	filter ServiceFilter
}

func (c *Config) serviceFilters() []namedFilter {
	var out []namedFilter
	g := c.Generate
	if g.GoServer != nil {
		out = append(out, namedFilter{"go-server", g.GoServer.ServiceFilter})
	}
	if g.GoClient != nil {
		out = append(out, namedFilter{"go-client", g.GoClient.ServiceFilter})
	}
	if g.TSClient != nil {
		out = append(out, namedFilter{"ts-client", g.TSClient.ServiceFilter})
	}
	if g.TSServer != nil {
		out = append(out, namedFilter{"ts-server", g.TSServer.ServiceFilter})
	}
	if g.PythonClient != nil {
		out = append(out, namedFilter{"python-client", g.PythonClient.ServiceFilter})
	}
	if g.DartClient != nil {
		out = append(out, namedFilter{"dart-client", g.DartClient.ServiceFilter})
	}
	if g.SwiftClient != nil {
		out = append(out, namedFilter{"swift-client", g.SwiftClient.ServiceFilter})
	}
	if g.RustClient != nil {
		out = append(out, namedFilter{"rust-client", g.RustClient.ServiceFilter})
	}
	if g.RustServer != nil {
		out = append(out, namedFilter{"rust-server", g.RustServer.ServiceFilter})
	}
	if g.OpenAPI != nil {
		out = append(out, namedFilter{"openapi", g.OpenAPI.ServiceFilter})
	}
	return out
}
