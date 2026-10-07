package httpkit

import "strings"

// ResolveGuard fills the ":name" segments of a @guard pattern from the
// request's path values, so "object/level/:id" becomes "object/level/42". It
// reports false when a placeholder has no value.
func ResolveGuard(pattern string, pathValue func(name string) string) (string, bool) {
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		name, ok := strings.CutPrefix(segment, ":")
		if !ok {
			continue
		}
		value := pathValue(name)
		if !safeGuardValue(value) {
			return "", false
		}
		segments[i] = value
	}
	return strings.Join(segments, "/"), true
}

// ResolveGuards resolves every pattern. The patterns come from the route's
// RequestMetadata.Guards and the path values from the request:
//
//	guards, ok := httpkit.ResolveGuards(meta.Guards, r.PathValue)
func ResolveGuards(patterns []string, pathValue func(name string) string) ([]string, bool) {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		resolved, ok := ResolveGuard(pattern, pathValue)
		if !ok {
			return nil, false
		}
		out = append(out, resolved)
	}
	return out, true
}

func safeGuardValue(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
