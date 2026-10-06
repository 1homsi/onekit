package gents

import (
	"regexp"
	"strings"
)

var (
	namedImportLine = regexp.MustCompile(`^import (type )?\{ ([^}]*) \} from ("[^"]*");$`)
	codecHeader     = regexp.MustCompile(`(?m)^export function (\w+)\(v: ([^)]*)\)([^\n]*) \{$`)
	identPattern    = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)
)

// stripStrings blanks the contents of string literals so identifiers and
// braces inside them are not mistaken for code.
func stripStrings(line string) string {
	var out strings.Builder
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case quote == 0:
			if r == '"' || r == '\'' || r == '`' {
				quote = r
			}
			out.WriteRune(r)
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == quote:
			quote = 0
			out.WriteRune(r)
		}
	}
	return out.String()
}

// identifierCounts returns how many times each identifier appears in source,
// ignoring import lines, which only name things, never use them, and string
// literals.
func identifierCounts(lines []string) map[string]int {
	counts := map[string]int{}
	for _, line := range lines {
		if strings.HasPrefix(line, "import ") {
			continue
		}
		for _, id := range identPattern.FindAllString(stripStrings(line), -1) {
			counts[id]++
		}
	}
	return counts
}

// pruneUnusedNamedImports drops names from single-line named imports that the
// rest of the file never mentions, and drops an import left with no names, so
// output compiles under noUnusedLocals.
func pruneUnusedNamedImports(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	counts := identifierCounts(lines)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		match := namedImportLine.FindStringSubmatch(line)
		if match == nil {
			out = append(out, line)
			continue
		}
		var kept []string
		for _, name := range strings.Split(match[2], ", ") {
			local := strings.TrimSpace(name)
			if _, alias, ok := strings.Cut(local, " as "); ok {
				local = strings.TrimSpace(alias)
			}
			if counts[local] > 0 {
				kept = append(kept, name)
			}
		}
		if len(kept) == 0 {
			continue
		}
		out = append(out, "import "+match[1]+"{ "+strings.Join(kept, ", ")+" } from "+match[3]+";")
	}
	return []byte(strings.Join(out, "\n"))
}

// declarationEnd returns the index of the last line of the declaration that
// starts at lines[start], or -1 when its braces never balance. A const is one
// line; a function ends where its braces balance. Braces inside the helpers'
// strings and regular expressions are always balanced, so counting is safe.
func declarationEnd(lines []string, start int) int {
	if strings.HasPrefix(lines[start], "const ") {
		return start
	}
	depth := 0
	opened := false
	for i := start; i < len(lines); i++ {
		code := stripStrings(lines[i])
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if strings.Contains(code, "{") {
			opened = true
		}
		if opened && depth <= 0 {
			return i
		}
	}
	return -1
}

// pruneUnusedHelpers removes private top-level helper declarations that
// nothing else in the file references. Removing one can orphan another, so it
// repeats until nothing changes.
func pruneUnusedHelpers(src []byte, names []string) []byte {
	lines := strings.Split(string(src), "\n")
	for {
		counts := identifierCounts(lines)
		removed := false
		for _, name := range names {
			if counts[name] != 1 {
				continue
			}
			start := -1
			for i, line := range lines {
				if strings.HasPrefix(line, "function "+name+"(") || strings.HasPrefix(line, "async function "+name+"(") || strings.HasPrefix(line, "const "+name+" ") {
					start = i
					break
				}
			}
			if start < 0 {
				continue
			}
			end := declarationEnd(lines, start)
			if end < 0 {
				continue
			}
			if end+1 < len(lines) && lines[end+1] == "" {
				end++
			}
			lines = append(lines[:start], lines[end+1:]...)
			removed = true
			break
		}
		if !removed {
			return []byte(strings.Join(lines, "\n"))
		}
	}
}

// nameUnusedCodecParameters renames the parameter of a generated codec
// function to _v when its body never reads it (the codecs of a message with
// no fields), which noUnusedParameters accepts.
func nameUnusedCodecParameters(src []byte) []byte {
	text := string(src)
	matches := codecHeader.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return src
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		headerEnd := m[1]
		end := strings.Index(text[headerEnd:], "\n}\n")
		if end < 0 {
			continue
		}
		body := text[headerEnd : headerEnd+end]
		if regexp.MustCompile(`\bv\b`).MatchString(body) {
			continue
		}
		header := text[m[0]:m[1]]
		out.WriteString(text[last:m[0]])
		out.WriteString(strings.Replace(header, "(v: ", "(_v: ", 1))
		last = m[1]
	}
	out.WriteString(text[last:])
	return []byte(out.String())
}

var clientHelpers = []string{"requestSignal", "readResponseText", "responseBodyLimit", "DEFAULT_MAX_RESPONSE_BODY_BYTES", "DEFAULT_MAX_SSE_LINE_BYTES"}

var serverHelpers = []string{"parseScalar", "validHeaderFormat", "readJSONBody", "maxRequestBodyBytes", "wildcardLast", "matchPath", "jsonResponse", "withAuthorization", "nodeHeaderValue", "renderError", "sseHeartbeats", "sseErrorWriters"}
