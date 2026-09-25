package onek

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

const (
	symbolKindMessage = "message"
	symbolKindEnum    = "enum"
)

var renameIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func renamable(symbol *LanguageSymbol) bool {
	return symbol != nil && (symbol.Kind == symbolKindMessage || symbol.Kind == symbolKindEnum)
}

func (s *LanguageSnapshot) nameRange(location Location, name string) (Range, bool) {
	lines := s.lines[location.Path]
	start, end := location.Range.Start, location.Range.End
	if start.Line != end.Line || start.Line >= len(lines) {
		return Range{}, false
	}
	units := utf16.Encode([]rune(lines[start.Line]))
	if end.Character > len(units) || start.Character > end.Character {
		return Range{}, false
	}
	text := string(utf16.Decode(units[start.Character:end.Character]))
	index := strings.LastIndex(text, name)
	if index < 0 {
		return Range{}, false
	}
	offset := start.Character + len(utf16.Encode([]rune(text[:index])))
	return Range{Position{start.Line, offset}, Position{start.Line, offset + len(utf16.Encode([]rune(name)))}}, true
}

func prepareRename(snapshot *LanguageSnapshot, symbol *LanguageSymbol, path string, at Position) any {
	if !renamable(symbol) {
		return nil
	}
	for _, location := range snapshot.References(symbol, true) {
		if location.Path != path || !contains(location.Range, at) {
			continue
		}
		if r, ok := snapshot.nameRange(location, symbol.Name); ok {
			return map[string]any{"range": r, "placeholder": symbol.Name}
		}
	}
	return nil
}

func renameSymbol(snapshot *LanguageSnapshot, symbol *LanguageSymbol, newName string) (any, *rpcError) {
	if !renamable(symbol) {
		return nil, &rpcError{-32602, "only messages and enums can be renamed"}
	}
	if !renameIdentifier.MatchString(newName) {
		return nil, &rpcError{-32602, "new name must be a letter or underscore followed by letters, digits, or underscores"}
	}
	for _, other := range snapshot.Symbols {
		if other != symbol && (other.Kind == symbolKindMessage || other.Kind == symbolKindEnum) && other.Name == newName &&
			strings.TrimSuffix(other.QualifiedName, other.Name) == strings.TrimSuffix(symbol.QualifiedName, symbol.Name) {
			return nil, &rpcError{-32602, "a type named " + newName + " already exists in this scope"}
		}
	}
	changes := map[string][]any{}
	for _, location := range snapshot.References(symbol, true) {
		r, ok := snapshot.nameRange(location, symbol.Name)
		if !ok {
			continue
		}
		uri := fileURI(location.Path)
		changes[uri] = append(changes[uri], map[string]any{"range": r, "newText": newName})
	}
	return map[string]any{"changes": changes}, nil
}
