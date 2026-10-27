package onek

import (
	"path/filepath"
	"strings"
)

// inferBasePath computes a service's default HTTP base_path from its source
// directory, given relative to the schema root (the directory containing
// onkt.toml). Each path segment is kebab-cased (snake_case -> kebab-case)
// to match the existing house style seen across services migrating from the
// old proto-based toolchain (e.g. hr/time_entry/v1 -> /hr/time-entry/v1).
// Only used when a service doesn't set base_path explicitly - an explicit
// value always wins.
func inferBasePath(relDir string) string {
	rel := filepath.ToSlash(relDir)
	if rel == "." || rel == "" {
		return "/"
	}

	segments := strings.Split(rel, "/")
	for i, seg := range segments {
		segments[i] = strings.ReplaceAll(seg, "_", "-")
	}
	return "/" + strings.Join(segments, "/")
}
