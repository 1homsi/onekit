package onkimport

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// text returns the string value stored at m[key], or "" for anything else.
func text(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// textOf returns any scalar value as its string form.
func textOf(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func firstMap(items []any) map[string]any {
	for _, item := range items {
		if m := asMap(item); m != nil {
			return m
		}
	}
	return nil
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// slug reduces an arbitrary string to [a-z0-9_], collapsing runs into _.
func slug(value, fallback string) string {
	var out strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && out.Len() > 0 {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	trimmed := strings.Trim(out.String(), "_")
	if trimmed == "" {
		if fallback == "" {
			return "api"
		}
		return fallback
	}
	if trimmed[0] >= '0' && trimmed[0] <= '9' {
		return fallback + "_" + trimmed
	}
	return trimmed
}

var identStart = regexp.MustCompile(`^[A-Za-z_]`)

// safeIdent preserves declared casing for identifier-safe names so path and
// query bindings keep matching their templates; only unsafe characters are
// replaced.
func safeIdent(name string) string {
	out := make([]rune, 0, len(name))
	for i, r := range name {
		switch {
		case r == '_' || unicode.IsLetter(r):
			out = append(out, r)
		case unicode.IsDigit(r):
			if i == 0 {
				out = append(out, 'f')
			}
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	cleaned := strings.Trim(string(out), "_")
	if cleaned == "" || !identStart.MatchString(cleaned) {
		return "f_" + strings.TrimLeft(cleaned, "_")
	}
	return cleaned
}

// upperSnake renders an enum member: letters/digits uppercased with runs of
// other characters collapsed to single underscores.
func upperSnake(value string) string {
	var out strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(unicode.ToUpper(r))
			lastUnderscore = false
		default:
			if !lastUnderscore && out.Len() > 0 {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(out.String(), "_")
}

// pascalIdent preserves declared casing while forcing identifier shape:
// invalid characters collapse to single underscores and a digit-leading or
// empty result gains an "X" prefix.
func pascalIdent(name string) string {
	var out strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			if out.Len() == 0 && unicode.IsDigit(r) {
				out.WriteByte('X')
			}
			out.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && out.Len() > 0 {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	cleaned := strings.Trim(out.String(), "_")
	if cleaned == "" {
		return "X"
	}
	return cleaned
}

// refCanonicalName derives a component schema's declaration name from its
// $ref pointer, preserving declared casing (PetStatus stays PetStatus).
func refCanonicalName(ref string) string {
	parts := strings.Split(ref, "/")
	return pascalIdent(parts[len(parts)-1])
}

// Pascal converts snake/kebab/space-delimited text to PascalCase.
func Pascal(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.' || r == '/'
	})
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		out.WriteString(string(runes))
	}
	return out.String()
}

func singular(name string) string {
	name = strings.TrimSuffix(name, "s")
	name = strings.TrimSuffix(name, "List")
	if name == "" {
		return "item"
	}
	return name
}

// urlPath extracts the path component of a server URL.
func urlPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	path := parsed.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimSuffix(path, "/")
}

func sortUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sortStrings(out)
	return out
}

func sortStrings(values []string) { sort.Strings(values) }

const scalarString = "string"

func isScalarExpr(expr string) bool {
	switch expr {
	case scalarString, "bool", "int32", "int64", "uint32", "uint64",
		"float32", "float64", "timestamp", "json":
		return true
	}
	return false
}

// uniqueName reserves a declaration name across messages and enums while
// preserving declared casing.
func (im *importer) uniqueName(base string) string {
	name := pascalIdent(base)
	if name == "" {
		name = "X"
	}
	candidate := name
	for i := 2; im.usedNames[candidate]; i++ {
		candidate = name + "_" + strconv.Itoa(i)
	}
	im.usedNames[candidate] = true
	return candidate
}

func (im *importer) warnf(format string, args ...any) {
	im.warnings = append(im.warnings, fmt.Sprintf(format, args...))
}

// allOfRequired collects the merged required list from an allOf array.
func (im *importer) allOfRequired(allOf []any) []any {
	var required []any
	for _, part := range allOf {
		sub := asMap(part)
		if sub == nil {
			continue
		}
		if ref := text(sub, "$ref"); ref != "" {
			sub = im.lookupRef(ref)
			if sub == nil {
				continue
			}
		}
		required = append(required, asSlice(sub["required"])...)
	}
	return required
}

// truthy interprets OpenAPI boolean fields arriving as YAML/JSON bools or
// legacy string spellings.
func truthy(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return false
	}
}

// composeFieldLine assembles "name: type[?] [suffix]" keeping the optional
// marker between the type and its decorators, which is where the .onk
// grammar expects it.
func composeFieldLine(name string, ft fieldType, optional bool, owner, rawName string) string {
	typeExpr := ft.expr
	marker := ""
	if optional {
		if strings.HasSuffix(typeExpr, "[]") {
			fmt.Fprintf(&strings.Builder{}, "") // no-op; warning handled by caller when needed
			marker = ""
		} else {
			marker = "?"
		}
	}
	line := name + ": " + typeExpr + marker + ft.suffix
	_ = owner
	_ = rawName
	return line
}
