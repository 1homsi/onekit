package onek

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

// Position and Range use the LSP convention: zero-based lines and UTF-16 columns.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type Location struct {
	Path  string `json:"path"`
	Range Range  `json:"range"`
}
type LanguageSymbol struct {
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualifiedName"`
	Kind          string   `json:"kind"`
	Detail        string   `json:"detail"`
	Documentation string   `json:"documentation,omitempty"`
	Location      Location `json:"location"`
}
type LanguageFile struct {
	Path    string   `json:"path"`
	Package string   `json:"package"`
	Imports []string `json:"imports"`
}
type languageReference struct {
	location Location
	target   *LanguageSymbol
}

// LanguageSnapshot is rebuilt from disk (plus editor overlays) for each request.
// References are emitted only after successful compilation, never guessed by name.
type LanguageSnapshot struct {
	Root        string            `json:"root"`
	Files       []LanguageFile    `json:"files"`
	Symbols     []*LanguageSymbol `json:"symbols"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
	texts       map[string]string
	lines       map[string][]string
	refs        []languageReference
}

func AnalyzeLanguage(dir string, overlays map[string]string) (*LanguageSnapshot, error) {
	project, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	s := &LanguageSnapshot{Root: project, Files: []LanguageFile{}, Symbols: []*LanguageSymbol{}, Diagnostics: []Diagnostic{}, texts: map[string]string{}, lines: map[string][]string{}}
	fail := func(err error) (*LanguageSnapshot, error) {
		s.Diagnostics = append(s.Diagnostics, Diagnostics(err)...)
		return s, nil
	}
	cfg, err := loadOptionalConfig(project)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return fail(err)
	}
	root := project
	options := onkcompile.CompileOptions{}
	if cfg != nil {
		root = cfg.SchemaDir()
		options.AllowLegacyContracts = cfg.AllowLegacyContracts
	}
	paths, err := discoverOnkFiles(root)
	if err != nil {
		return fail(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		seen[path] = true
	}
	for path, text := range overlays {
		if !filepath.IsAbs(path) || !pathWithin(root, path) || filepath.Ext(path) != ".onk" {
			return nil, fmt.Errorf("overlay must be a .onk file inside the schema root: %s", path)
		}
		if len(text) > maxInputFileBytes {
			return nil, fmt.Errorf("overlay exceeds input limit: %s", path)
		}
		if err := rejectSymlinkPath(path); err != nil {
			return nil, err
		}
		if err := validateSchemaPath(root, path); err != nil {
			return nil, err
		}
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) > maxInputFileCount {
		return nil, errors.New("too many schema files")
	}
	if len(paths) == 0 {
		return fail(fmt.Errorf("no .onk files found under %s", root))
	}
	sources := []onkcompile.Source{}
	for _, path := range paths {
		text, ok := overlays[path]
		if !ok {
			data, readErr := readRegularFile(path)
			if readErr != nil {
				return fail(readErr)
			}
			text = string(data)
		}
		s.texts[path] = text
		s.lines[path] = strings.Split(text, "\n")
		ast, parseErr := onklang.Parse(text)
		if parseErr != nil {
			s.Diagnostics = append(s.Diagnostics, Diagnostics(&ParseDiagnosticError{Path: path, Err: parseErr})...)
			continue
		}
		sources = append(sources, onkcompile.Source{Path: path, AST: ast})
		imports := append([]string{}, ast.Imports...)
		s.Files = append(s.Files, LanguageFile{Path: path, Package: ast.Package, Imports: imports})
	}
	return s.indexSources(sources, root, options)
}

func (s *LanguageSnapshot) indexSources(sources []onkcompile.Source, root string, options onkcompile.CompileOptions) (*LanguageSnapshot, error) {
	fail := func(err error) (*LanguageSnapshot, error) {
		s.Diagnostics = append(s.Diagnostics, Diagnostics(err)...)
		return s, nil
	}
	// Declarations remain discoverable while a different file is broken.
	declarations := map[any]*LanguageSymbol{}
	for _, src := range sources {
		s.addDeclarations(src, declarations)
	}
	if len(s.Diagnostics) > 0 {
		return s, nil
	}
	pkg, compileErr := onkcompile.CompileWithOptions(sources, options)
	if compileErr != nil {
		return fail(compileErr)
	}
	if err := applyDefaultBasePaths(pkg, root); err != nil {
		return fail(err)
	}
	targets := map[any]*LanguageSymbol{}
	var bindMessage func(*onklang.MessageDecl, *onkir.Message)
	bindEnum := func(a *onklang.EnumDecl, b *onkir.Enum) { targets[b] = declarations[a] }
	bindMessage = func(a *onklang.MessageDecl, b *onkir.Message) {
		targets[b] = declarations[a]
		for i, n := range a.Nested {
			bindMessage(n, b.Nested[i])
		}
		for i, n := range a.NestedEn {
			bindEnum(n, b.NestedEnums[i])
		}
	}
	for i, src := range sources {
		for j, a := range src.AST.Messages {
			bindMessage(a, pkg.Files[i].Messages[j])
		}
		for j, a := range src.AST.Enums {
			bindEnum(a, pkg.Files[i].Enums[j])
		}
	}
	add := func(path string, span onklang.Span, target any) {
		if symbol := targets[target]; symbol != nil {
			s.refs = append(s.refs, languageReference{Location{path, s.sourceRange(path, span)}, symbol})
		}
	}
	var addType func(string, *onklang.TypeRef, *onkir.Type)
	addType = func(path string, a *onklang.TypeRef, b *onkir.Type) {
		if a == nil || b == nil {
			return
		}
		switch b.Kind {
		case onkir.KindMap:
			addType(path, a.MapVal, b.MapValue)
		case onkir.KindMessage:
			add(path, a.Span, b.Message)
		case onkir.KindEnum:
			add(path, a.Span, b.Enum)
		}
	}
	var refsMessage func(string, *onklang.MessageDecl, *onkir.Message)
	refsMessage = func(path string, a *onklang.MessageDecl, b *onkir.Message) {
		for i, f := range a.Fields {
			if f.Oneof != nil {
				for j, v := range f.Oneof.Variants {
					addType(path, v.Type, b.Fields[i].Oneof.Variants[j].Type)
				}
			} else {
				addType(path, f.Type, b.Fields[i].Type)
			}
		}
		for i, n := range a.Nested {
			refsMessage(path, n, b.Nested[i])
		}
	}
	for i, src := range sources {
		for j, a := range src.AST.Messages {
			refsMessage(src.Path, a, pkg.Files[i].Messages[j])
		}
		for j, a := range src.AST.Services {
			for k, r := range a.RPCs {
				method := pkg.Files[i].Services[j].Methods[k]
				add(src.Path, r.RequestSpan, method.Request)
				add(src.Path, r.ResponseSpan, method.Response)
				for n, span := range r.ErrorSpans {
					add(src.Path, span, method.ErrorTypes[n])
				}
			}
		}
	}
	return s, nil
}

func (s *LanguageSnapshot) sourceRange(path string, span onklang.Span) Range {
	lines := s.lines[path]
	position := func(line, col int) Position {
		line = max(line-1, 0)
		col = max(col-1, 0)
		if line < len(lines) {
			col = len(utf16.Encode([]rune(lines[line][:min(col, len(lines[line]))])))
		} else {
			col = 0
		}
		return Position{line, col}
	}
	return Range{position(span.Line, span.Col), position(span.EndLine, span.EndCol)}
}

func (s *LanguageSnapshot) addDeclarations(src onkcompile.Source, decls map[any]*LanguageSymbol) {
	add := func(node any, name, qualified, kind, detail, doc string, line, col int) {
		symbol := &LanguageSymbol{Name: name, QualifiedName: qualified, Kind: kind, Detail: detail, Documentation: doc, Location: Location{src.Path, s.sourceRange(src.Path, onklang.Span{Line: line, Col: col, EndLine: line, EndCol: col + len(name)})}}
		s.Symbols = append(s.Symbols, symbol)
		decls[node] = symbol
	}
	qualify := func(parent, name string) string {
		if parent == "" {
			return name
		}
		return parent + "." + name
	}
	enum := func(e *onklang.EnumDecl, parent string) {
		q := qualify(parent, e.Name)
		names := []string{}
		for _, v := range e.Values {
			names = append(names, v.Name)
		}
		add(e, e.Name, q, "enum", "enum "+q+" { "+strings.Join(names, ", ")+" }", e.Doc, e.Line, e.Col)
		for i := range e.Values {
			v := &e.Values[i]
			add(v, v.Name, qualify(q, v.Name), "enumMember", v.Name, v.Doc, v.Line, v.Col)
		}
	}
	var message func(*onklang.MessageDecl, string)
	message = func(m *onklang.MessageDecl, parent string) {
		q := qualify(parent, m.Name)
		fields := []string{}
		for _, f := range m.Fields {
			fields = append(fields, f.Name+": "+fieldType(f))
		}
		add(m, m.Name, q, "message", "message "+q+" { "+strings.Join(fields, ", ")+" }", m.Doc, m.Line, m.Col)
		for _, f := range m.Fields {
			add(f, f.Name, qualify(q, f.Name), "field", f.Name+": "+fieldType(f)+onklang.FormatDecorators(f.Decorators), withDecoratorDocs(f.Doc, f.Decorators), f.Line, f.Col)
			if f.Oneof != nil {
				for i := range f.Oneof.Variants {
					v := &f.Oneof.Variants[i]
					add(v, v.Name, qualify(qualify(q, f.Name), v.Name), "field", v.Name+": "+typeName(v.Type)+onklang.FormatDecorators(v.Decorators), withDecoratorDocs("", v.Decorators), v.Line, v.Col)
				}
			}
		}
		for _, n := range m.Nested {
			message(n, q)
		}
		for _, e := range m.NestedEn {
			enum(e, q)
		}
	}
	for _, m := range src.AST.Messages {
		message(m, src.AST.Package)
	}
	for _, e := range src.AST.Enums {
		enum(e, src.AST.Package)
	}
	for _, service := range src.AST.Services {
		q := qualify(src.AST.Package, service.Name)
		methods := []string{}
		for _, r := range service.RPCs {
			methods = append(methods, r.Name)
		}
		add(service, service.Name, q, "service", "service "+q+" { "+strings.Join(methods, ", ")+" }", service.Doc, service.Line, service.Col)
		for _, r := range service.RPCs {
			detail := r.Name + "(" + r.RequestType + ") -> " + r.ResponseType
			if len(r.ErrorTypes) > 0 {
				detail += " | " + strings.Join(r.ErrorTypes, " | ")
			}
			detail += onklang.FormatDecorators(r.Decorators)
			add(r, r.Name, qualify(q, r.Name), "method", detail, withDecoratorDocs(r.Doc, r.Decorators), r.Line, r.Col)
		}
	}
}
func typeName(t *onklang.TypeRef) string {
	if t == nil {
		return "oneof"
	}
	if t.IsMap {
		return "map[" + t.MapKey + ", " + typeName(t.MapVal) + "]"
	}
	return t.Name
}
func fieldType(f *onklang.FieldDecl) string {
	name := typeName(f.Type)
	if f.Repeated {
		name = "[]" + name
	}
	if f.Optional {
		name += "?"
	}
	return name
}
func before(a, b Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
}
func contains(r Range, p Position) bool { return !before(p, r.Start) && before(p, r.End) }
func (s *LanguageSnapshot) SymbolAt(path string, p Position) *LanguageSymbol {
	for _, r := range s.refs {
		if r.location.Path == path && contains(r.location.Range, p) {
			return r.target
		}
	}
	for _, symbol := range s.Symbols {
		if symbol.Location.Path == path && contains(symbol.Location.Range, p) {
			return symbol
		}
	}
	return nil
}
func (s *LanguageSnapshot) References(symbol *LanguageSymbol, includeDeclaration bool) []Location {
	result := []Location{}
	if symbol == nil {
		return result
	}
	if includeDeclaration {
		result = append(result, symbol.Location)
	}
	for _, r := range s.refs {
		if r.target == symbol {
			result = append(result, r.location)
		}
	}
	return result
}
func (s *LanguageSnapshot) Search(query, path string) []*LanguageSymbol {
	result := []*LanguageSymbol{}
	for _, symbol := range s.Symbols {
		if (path == "" || symbol.Location.Path == path) && strings.Contains(strings.ToLower(symbol.QualifiedName), strings.ToLower(query)) {
			result = append(result, symbol)
		}
	}
	return result
}
func languagePath(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !pathWithin(root, path) || filepath.Ext(path) != ".onk" {
		return "", errors.New("path must be a .onk file inside the project")
	}
	if err := rejectSymlinkPath(path); err != nil {
		return "", err
	}
	return path, nil
}

var decoratorDocs = map[string]string{
	"ws":         "@ws(path): a bidirectional WebSocket RPC. The request and response messages are the client-to-server and server-to-client frame types.",
	"ws_id":      "@ws_id: the correlation key of a @ws frame. call(id, value) sends a frame and resolves with the reply carrying the same id in a different oneof variant.",
	"ws_cancel":  "@ws_cancel: the oneof variant sent when a correlated call is abandoned (cancelled or timed out). Its message must carry the @ws_id; it always reaches the peer's handler.",
	"ws_timeout": "@ws_timeout: an integer field next to a @ws_id that generated calls fill with the caller's remaining time in milliseconds when they carry a deadline.",
	"raw":        "@raw: a string or bytes field carried outside the JSON as raw bytes in a binary WebSocket frame, so large payloads are never escaped or scanned.",
	"tag":        "@tag(value): the discriminator value that identifies this oneof variant on the wire.",
	"encode":     "@encode(kind): the field's wire encoding (number, hex, base64, base64_raw, base64url, base64url_raw, unix_seconds, unix_millis, date).",
	"required":   "@required: the field must be present and non-empty.",
	"query":      "@query(name): bind the field to a URL query parameter.",
	"flatten":    "@flatten(prefix): inline the child message's fields into this message on the wire.",
}

func withDecoratorDocs(doc string, decorators []onklang.Decorator) string {
	parts := []string{}
	if doc != "" {
		parts = append(parts, doc)
	}
	for _, d := range decorators {
		if text, ok := decoratorDocs[d.Name]; ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func DecoratorCompletions(prefix string) []map[string]any {
	names := make([]string, 0, len(decoratorDocs))
	for name := range decoratorDocs {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		items = append(items, map[string]any{"label": "@" + name, "insertText": name, "kind": 14, "documentation": decoratorDocs[name]})
	}
	return items
}
