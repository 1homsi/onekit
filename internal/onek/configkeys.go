package onek

import (
	"reflect"
	"strings"
)

func knownConfigKeys() []string {
	var keys []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}
		for i := range t.NumField() {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
			if name == "" || name == "-" {
				continue
			}
			keys = append(keys, prefix+name)
			walk(t.Field(i).Type, prefix+name+".")
		}
	}
	walk(reflect.TypeFor[Config](), "")
	return keys
}

func describeUnknownKeys(unknown []string) []string {
	reported := map[string]bool{}
	var out []string
	known := knownConfigKeys()
	for _, key := range unknown {
		parent := key
		skip := false
		for i := strings.LastIndex(parent, "."); i > 0; i = strings.LastIndex(parent, ".") {
			parent = parent[:i]
			if reported[parent] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		reported[key] = true
		if suggestion := closestConfigKey(key, known); suggestion != "" {
			key += " (did you mean " + suggestion + "?)"
		}
		out = append(out, key)
	}
	return out
}

func closestConfigKey(key string, known []string) string {
	prefix, leaf := "", key
	if i := strings.LastIndex(key, "."); i >= 0 {
		prefix, leaf = key[:i+1], key[i+1:]
	}
	best, bestScore := "", 4
	for _, candidate := range known {
		if !strings.HasPrefix(candidate, prefix) || strings.Contains(candidate[len(prefix):], ".") {
			continue
		}
		name := candidate[len(prefix):]
		score := editDistance(strings.ToLower(leaf), name)
		if strings.HasPrefix(name, strings.ToLower(leaf)) || strings.HasPrefix(strings.ToLower(leaf), name) {
			score = min(score, 1)
		}
		if score < bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
