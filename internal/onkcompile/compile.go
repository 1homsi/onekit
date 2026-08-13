package onkcompile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

type Error struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Code   string `json:"code,omitempty"`
	Msg    string `json:"message"`
}

func (e *Error) Error() string {
	if e.Column > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Path, e.Line, e.Column, e.Msg)
	}
	return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
}

type Source struct {
	Path string
	AST  *onklang.File
}

// CompileOptions controls compatibility behavior for existing contracts.
// Legacy contracts may contain scalar @required annotations and member names
// that are valid for their configured generators but fail newer cross-target
// validation rules.
type CompileOptions struct {
	AllowLegacyContracts bool
}

// validateAndBuildImportScopes makes the import syntax meaningful without
// breaking legacy schemas that omit imports. A file with imports may resolve
// declarations from its own directory and the imported files (transitively);
// files without imports retain the historical project-wide lookup behavior.
func validateAndBuildImportScopes(sources []Source) (map[string]map[string]bool, error) {
	byPath := make(map[string]Source, len(sources))
	for _, source := range sources {
		path := filepath.Clean(source.Path)
		if _, exists := byPath[path]; exists {
			return nil, &Error{Path: source.Path, Msg: "duplicate source path"}
		}
		byPath[path] = source
	}

	graph := make(map[string][]string)
	for _, source := range sources {
		path := filepath.Clean(source.Path)
		seen := map[string]bool{}
		for _, importPath := range source.AST.Imports {
			if importPath == "" {
				return nil, &Error{Path: source.Path, Msg: "import path must not be empty"}
			}
			if filepath.IsAbs(importPath) {
				return nil, &Error{Path: source.Path, Msg: fmt.Sprintf("import %q must be relative", importPath)}
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(importPath)))
			if _, exists := byPath[target]; !exists {
				return nil, &Error{Path: source.Path, Msg: fmt.Sprintf("import %q does not match a discovered .onk file", importPath)}
			}
			if seen[target] {
				return nil, &Error{Path: source.Path, Msg: fmt.Sprintf("duplicate import %q", importPath)}
			}
			seen[target] = true
			graph[path] = append(graph[path], target)
		}
	}

	state := map[string]int{}
	var visit func(string) error
	visit = func(path string) error {
		switch state[path] {
		case 1:
			return &Error{Path: path, Msg: "cyclic schema import"}
		case 2:
			return nil
		}
		state[path] = 1
		for _, target := range graph[path] {
			if err := visit(target); err != nil {
				return err
			}
		}
		state[path] = 2
		return nil
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := visit(path); err != nil {
			return nil, err
		}
	}

	var scopes map[string]map[string]bool
	computed := map[string]bool{}
	var scope func(string) map[string]bool
	scope = func(path string) map[string]bool {
		if computed[path] {
			return scopes[path]
		}
		computed[path] = true
		result := map[string]bool{filepath.Dir(path): true}
		for _, target := range graph[path] {
			for dir := range scope(target) {
				result[dir] = true
			}
		}
		if len(graph[path]) > 0 {
			scopes[path] = result
		}
		return result
	}
	scopes = make(map[string]map[string]bool)
	for _, path := range paths {
		if len(graph[path]) > 0 {
			scope(path)
		}
	}
	return scopes, nil
}

// dirMsg/dirEnum pair a declaration with the source directory it came from,
// used to resolve cross-directory references and report ambiguous ones.
type dirMsg struct {
	dir string
	msg *onkir.Message
}

type dirEnum struct {
	dir  string
	enum *onkir.Enum
}

// compiler enforces message/enum name uniqueness per source directory (one
// directory = one generated package, see internal/onek's sourceIndex) rather
// than across the whole schema tree, since a project with many independent
// services will naturally reuse common names like "GetDashboardRequest"
// across unrelated directories. A name not found in the referencing file's
// own directory falls back to a project-wide search, so cross-directory
// references still resolve without import statements - it's only an error
// when that name is ambiguous (declared in more than one other directory).
type compiler struct {
	msgByDir      map[string]map[string]*onkir.Message
	enumByDir     map[string]map[string]*onkir.Enum
	msgAllByName  map[string][]dirMsg
	enumAllByName map[string][]dirEnum
	declByDir     map[string]map[string]string
	msgNode       map[*onklang.MessageDecl]*onkir.Message
	enumNode      map[*onklang.EnumDecl]*onkir.Enum
	importScopes  map[string]map[string]bool
}

func Compile(sources []Source) (*onkir.Package, error) {
	return CompileWithOptions(sources, CompileOptions{})
}

// CompileWithOptions compiles a contract while keeping structural validation
// enabled. AllowLegacyContracts only relaxes the newer scalar-presence and
// Python-member-name checks so existing Go/TypeScript contract trees can move
// to newer generators without changing their wire model in one step.
func CompileWithOptions(sources []Source, options CompileOptions) (*onkir.Package, error) {
	if err := validateSyntax(sources, options); err != nil {
		return nil, err
	}
	importScopes, err := validateAndBuildImportScopes(sources)
	if err != nil {
		return nil, err
	}

	c := &compiler{
		msgByDir:      map[string]map[string]*onkir.Message{},
		enumByDir:     map[string]map[string]*onkir.Enum{},
		msgAllByName:  map[string][]dirMsg{},
		enumAllByName: map[string][]dirEnum{},
		declByDir:     map[string]map[string]string{},
		msgNode:       map[*onklang.MessageDecl]*onkir.Message{},
		enumNode:      map[*onklang.EnumDecl]*onkir.Enum{},
		importScopes:  importScopes,
	}

	var files []*onkir.File
	for _, src := range sources {
		f := &onkir.File{Path: src.Path, Package: src.AST.Package, Imports: src.AST.Imports}
		files = append(files, f)

		for _, md := range src.AST.Messages {
			m, err := c.declareMessage(md, f, nil, src.Path)
			if err != nil {
				return nil, err
			}
			f.Messages = append(f.Messages, m)
		}
		for _, ed := range src.AST.Enums {
			e, err := c.declareEnum(ed, f, nil, src.Path)
			if err != nil {
				return nil, err
			}
			f.Enums = append(f.Enums, e)
		}
	}

	for i, src := range sources {
		f := files[i]
		for _, md := range src.AST.Messages {
			if err := c.fillMessage(md, src.Path); err != nil {
				return nil, err
			}
		}
		for _, sd := range src.AST.Services {
			s, err := c.buildService(sd, f, src.Path)
			if err != nil {
				return nil, err
			}
			f.Services = append(f.Services, s)
		}
	}

	pkg := &onkir.Package{Files: files}
	if err := validateContract(pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func (c *compiler) declareMessage(
	md *onklang.MessageDecl,
	f *onkir.File,
	parent *onkir.Message,
	path string,
) (*onkir.Message, error) {
	dir := filepath.Dir(path)
	if _, exists := c.msgByDir[dir][md.Name]; exists {
		return nil, &Error{Path: path, Line: md.Line, Column: md.Col, Msg: fmt.Sprintf("duplicate message name %q", md.Name)}
	}
	if _, exists := c.enumByDir[dir][md.Name]; exists {
		return nil, &Error{Path: path, Line: md.Line, Column: md.Col, Msg: fmt.Sprintf("name %q already used by an enum", md.Name)}
	}
	generated := generatedIdentifier(md.Name)
	if previous := c.declByDir[dir][generated]; previous != "" {
		return nil, &Error{Path: path, Line: md.Line, Column: md.Col, Msg: fmt.Sprintf(
			"message name %q collides with %q after target-language name conversion", md.Name, previous,
		)}
	}

	m := &onkir.Message{Name: md.Name, Doc: md.Doc, File: f, Parent: parent}
	m.SchemaName = declarationFullName(f.Package, parent, md.Name)
	m.Decorators = convertDecorators(md.Decorators)
	if c.msgByDir[dir] == nil {
		c.msgByDir[dir] = map[string]*onkir.Message{}
	}
	c.msgByDir[dir][md.Name] = m
	if c.declByDir[dir] == nil {
		c.declByDir[dir] = map[string]string{}
	}
	c.declByDir[dir][generated] = md.Name
	c.msgAllByName[md.Name] = append(c.msgAllByName[md.Name], dirMsg{dir: dir, msg: m})
	c.msgNode[md] = m

	for _, nested := range md.Nested {
		nm, err := c.declareMessage(nested, f, m, path)
		if err != nil {
			return nil, err
		}
		m.Nested = append(m.Nested, nm)
	}
	for _, nested := range md.NestedEn {
		ne, err := c.declareEnum(nested, f, m, path)
		if err != nil {
			return nil, err
		}
		m.NestedEnums = append(m.NestedEnums, ne)
	}

	return m, nil
}

func (c *compiler) declareEnum(
	ed *onklang.EnumDecl,
	f *onkir.File,
	parent *onkir.Message,
	path string,
) (*onkir.Enum, error) {
	dir := filepath.Dir(path)
	if _, exists := c.enumByDir[dir][ed.Name]; exists {
		return nil, &Error{Path: path, Line: ed.Line, Column: ed.Col, Msg: fmt.Sprintf("duplicate enum name %q", ed.Name)}
	}
	if _, exists := c.msgByDir[dir][ed.Name]; exists {
		return nil, &Error{Path: path, Line: ed.Line, Column: ed.Col, Msg: fmt.Sprintf("name %q already used by a message", ed.Name)}
	}
	generated := generatedIdentifier(ed.Name)
	if previous := c.declByDir[dir][generated]; previous != "" {
		return nil, &Error{Path: path, Line: ed.Line, Column: ed.Col, Msg: fmt.Sprintf(
			"enum name %q collides with %q after target-language name conversion", ed.Name, previous,
		)}
	}

	e := &onkir.Enum{Name: ed.Name, Doc: ed.Doc, File: f, Parent: parent}
	e.SchemaName = declarationFullName(f.Package, parent, ed.Name)
	if c.enumByDir[dir] == nil {
		c.enumByDir[dir] = map[string]*onkir.Enum{}
	}
	c.enumByDir[dir][ed.Name] = e
	if c.declByDir[dir] == nil {
		c.declByDir[dir] = map[string]string{}
	}
	c.declByDir[dir][generated] = ed.Name
	c.enumAllByName[ed.Name] = append(c.enumAllByName[ed.Name], dirEnum{dir: dir, enum: e})
	c.enumNode[ed] = e

	for i, vd := range ed.Values {
		e.Values = append(e.Values, &onkir.EnumValue{
			Name:       vd.Name,
			Doc:        vd.Doc,
			Decorators: convertDecorators(vd.Decorators),
			Enum:       e,
			Index:      i,
		})
	}

	return e, nil
}

func declarationFullName(packageName string, parent *onkir.Message, name string) string {
	if parent != nil {
		return parent.FullName() + "." + name
	}
	if packageName != "" {
		return packageName + "." + name
	}
	return name
}

func (c *compiler) fillMessage(md *onklang.MessageDecl, path string) error {
	m := c.msgNode[md]
	for _, fd := range md.Fields {
		field, err := c.buildField(fd, m, path)
		if err != nil {
			return err
		}
		m.Fields = append(m.Fields, field)
	}
	for _, nested := range md.Nested {
		if err := c.fillMessage(nested, path); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) buildField(fd *onklang.FieldDecl, owner *onkir.Message, path string) (*onkir.Field, error) {
	field := &onkir.Field{
		Name:       fd.Name,
		Doc:        fd.Doc,
		Repeated:   fd.Repeated,
		Optional:   fd.Optional,
		Decorators: convertDecorators(fd.Decorators),
		Message:    owner,
	}

	if fd.Oneof != nil {
		oneof, err := c.buildOneof(fd.Oneof, field, path)
		if err != nil {
			return nil, err
		}
		field.Oneof = oneof
		return field, nil
	}

	typ, err := c.resolveType(fd.Type, path, fd.Line, fd.Col)
	if err != nil {
		return nil, err
	}
	field.Type = typ
	return field, nil
}

func (c *compiler) buildOneof(od *onklang.OneofDecl, field *onkir.Field, path string) (*onkir.Oneof, error) {
	oneof := &onkir.Oneof{Field: field, Args: convertArgs(od.Args)}
	for _, vd := range od.Variants {
		typ, err := c.resolveType(vd.Type, path, vd.Line, vd.Col)
		if err != nil {
			return nil, err
		}
		oneof.Variants = append(oneof.Variants, &onkir.OneofVariant{
			Name:       vd.Name,
			Type:       typ,
			Decorators: convertDecorators(vd.Decorators),
			Oneof:      oneof,
		})
	}
	return oneof, nil
}

// lookupMessage resolves a message name against the given directory first,
// then falls back to a project-wide search across every other directory
// (this is what lets cross-directory references work without an import
// statement). Returns a non-nil error only for a genuine ambiguity - the
// same name declared in more than one *other* directory; "not found" is
// signaled by found=false so callers can phrase their own "unresolved ..."
// message.
func (c *compiler) lookupMessage(dir, name string) (*onkir.Message, bool, error) {
	if m, ok := c.msgByDir[dir][name]; ok {
		return m, true, nil
	}
	matches := c.msgAllByName[name]
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0].msg, true, nil
	default:
		return nil, false, fmt.Errorf("ambiguous type %q found in multiple directories: %s", name, dirsOfMsg(matches))
	}
}

func (c *compiler) lookupEnum(dir, name string) (*onkir.Enum, bool, error) {
	if e, ok := c.enumByDir[dir][name]; ok {
		return e, true, nil
	}
	matches := c.enumAllByName[name]
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0].enum, true, nil
	default:
		return nil, false, fmt.Errorf("ambiguous type %q found in multiple directories: %s", name, dirsOfEnum(matches))
	}
}

func (c *compiler) lookupMessageForSource(path, name string) (*onkir.Message, bool, error) {
	if scope, restricted := c.importScopes[filepath.Clean(path)]; restricted {
		matches := make([]dirMsg, 0)
		for _, match := range c.msgAllByName[name] {
			if scope[match.dir] {
				matches = append(matches, match)
			}
		}
		switch len(matches) {
		case 0:
			return nil, false, nil
		case 1:
			return matches[0].msg, true, nil
		default:
			return nil, false, fmt.Errorf("ambiguous imported type %q found in multiple directories: %s", name, dirsOfMsg(matches))
		}
	}
	return c.lookupMessage(filepath.Dir(path), name)
}

func (c *compiler) lookupEnumForSource(path, name string) (*onkir.Enum, bool, error) {
	if scope, restricted := c.importScopes[filepath.Clean(path)]; restricted {
		matches := make([]dirEnum, 0)
		for _, match := range c.enumAllByName[name] {
			if scope[match.dir] {
				matches = append(matches, match)
			}
		}
		switch len(matches) {
		case 0:
			return nil, false, nil
		case 1:
			return matches[0].enum, true, nil
		default:
			return nil, false, fmt.Errorf("ambiguous imported type %q found in multiple directories: %s", name, dirsOfEnum(matches))
		}
	}
	return c.lookupEnum(filepath.Dir(path), name)
}

func dirsOfMsg(matches []dirMsg) string {
	dirs := make([]string, len(matches))
	for i, m := range matches {
		dirs[i] = m.dir
	}
	return strings.Join(dirs, ", ")
}

func dirsOfEnum(matches []dirEnum) string {
	dirs := make([]string, len(matches))
	for i, m := range matches {
		dirs[i] = m.dir
	}
	return strings.Join(dirs, ", ")
}

// lookupQualifiedMessage resolves a package-qualified type reference (e.g.
// "crm.customer.v1.Customer", written as such in a field type) against the
// declared `package` line of every source file, not against the source
// directory. This is the escape hatch for a genuine ambiguity: two
// directories that both need a plain-named type also declared identically
// elsewhere (e.g. two unrelated "Customer" messages) can each keep their
// plain name, and only the reference that would otherwise be ambiguous needs
// to spell out which package it means.
func (c *compiler) lookupQualifiedMessage(name string) (*onkir.Message, bool) {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return nil, false
	}
	pkg, simple := name[:dot], name[dot+1:]
	for _, matches := range c.msgAllByName[simple] {
		if matches.msg.File != nil && matches.msg.File.Package == pkg {
			return matches.msg, true
		}
	}
	return nil, false
}

func (c *compiler) lookupQualifiedEnum(name string) (*onkir.Enum, bool) {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return nil, false
	}
	pkg, simple := name[:dot], name[dot+1:]
	for _, matches := range c.enumAllByName[simple] {
		if matches.enum.File != nil && matches.enum.File.Package == pkg {
			return matches.enum, true
		}
	}
	return nil, false
}

func (c *compiler) lookupQualifiedMessageForSource(path, name string) (*onkir.Message, bool) {
	message, found := c.lookupQualifiedMessage(name)
	if !found {
		return nil, false
	}
	if scope, restricted := c.importScopes[filepath.Clean(path)]; restricted {
		if message.File == nil || !scope[filepath.Dir(message.File.Path)] {
			return nil, false
		}
	}
	return message, true
}

func (c *compiler) lookupQualifiedEnumForSource(path, name string) (*onkir.Enum, bool) {
	enum, found := c.lookupQualifiedEnum(name)
	if !found {
		return nil, false
	}
	if scope, restricted := c.importScopes[filepath.Clean(path)]; restricted {
		if enum.File == nil || !scope[filepath.Dir(enum.File.Path)] {
			return nil, false
		}
	}
	return enum, true
}

func (c *compiler) resolveType(t *onklang.TypeRef, path string, line, column int) (*onkir.Type, error) {
	if t.IsMap {
		keyKind, ok := onkir.ParseScalarKind(t.MapKey)
		if !ok {
			return nil, &Error{Path: path, Line: line, Column: column, Msg: fmt.Sprintf("invalid map key type %q", t.MapKey)}
		}
		if keyKind != onkir.ScalarString {
			return nil, &Error{Path: path, Line: line, Column: column, Msg: "map keys must be string for JSON and target-language parity"}
		}
		val, err := c.resolveType(t.MapVal, path, line, column)
		if err != nil {
			return nil, err
		}
		return &onkir.Type{Kind: onkir.KindMap, MapKey: keyKind, MapValue: val}, nil
	}

	if scalar, ok := onkir.ParseScalarKind(t.Name); ok {
		return &onkir.Type{Kind: onkir.KindScalar, Scalar: scalar}, nil
	}

	if strings.Contains(t.Name, ".") {
		if m, found := c.lookupQualifiedMessageForSource(path, t.Name); found {
			return &onkir.Type{Kind: onkir.KindMessage, Message: m}, nil
		}
		if e, found := c.lookupQualifiedEnumForSource(path, t.Name); found {
			return &onkir.Type{Kind: onkir.KindEnum, Enum: e}, nil
		}
		return nil, &Error{Path: path, Line: line, Column: column, Msg: fmt.Sprintf("unresolved qualified type %q", t.Name)}
	}

	m, found, err := c.lookupMessageForSource(path, t.Name)
	if err != nil {
		return nil, &Error{Path: path, Line: line, Column: column, Msg: err.Error()}
	}
	if found {
		return &onkir.Type{Kind: onkir.KindMessage, Message: m}, nil
	}
	e, found, err := c.lookupEnumForSource(path, t.Name)
	if err != nil {
		return nil, &Error{Path: path, Line: line, Column: column, Msg: err.Error()}
	}
	if found {
		return &onkir.Type{Kind: onkir.KindEnum, Enum: e}, nil
	}
	return nil, &Error{Path: path, Line: line, Column: column, Msg: fmt.Sprintf("unresolved type %q", t.Name)}
}

func (c *compiler) resolveMessageReference(path, name string) (*onkir.Message, bool, error) {
	if strings.Contains(name, ".") {
		message, found := c.lookupQualifiedMessageForSource(path, name)
		return message, found, nil
	}
	return c.lookupMessageForSource(path, name)
}

func (c *compiler) buildService(sd *onklang.ServiceDecl, f *onkir.File, path string) (*onkir.Service, error) {
	s := &onkir.Service{Name: sd.Name, Doc: sd.Doc, BasePath: sd.BasePath, File: f}
	headers, err := c.buildHeaders(sd.Headers, path)
	if err != nil {
		return nil, err
	}
	s.Headers = headers

	for _, rd := range sd.RPCs {
		method, err := c.buildMethod(rd, s, path)
		if err != nil {
			return nil, err
		}
		s.Methods = append(s.Methods, method)
	}
	return s, nil
}

func (c *compiler) buildMethod(rd *onklang.RPCDecl, s *onkir.Service, path string) (*onkir.Method, error) {
	req, found, err := c.resolveMessageReference(path, rd.RequestType)
	if err != nil {
		return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: err.Error()}
	}
	if !found {
		return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: fmt.Sprintf("unresolved request type %q", rd.RequestType)}
	}
	resp, found, err := c.resolveMessageReference(path, rd.ResponseType)
	if err != nil {
		return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: err.Error()}
	}
	if !found {
		return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: fmt.Sprintf("unresolved response type %q", rd.ResponseType)}
	}
	headers, err := c.buildHeaders(rd.Headers, path)
	if err != nil {
		return nil, err
	}

	method := &onkir.Method{
		Name:       rd.Name,
		Doc:        rd.Doc,
		Request:    req,
		Response:   resp,
		Decorators: convertDecorators(rd.Decorators),
		Headers:    headers,
		Service:    s,
	}

	seenStatuses := map[int]string{}
	for _, errName := range rd.ErrorTypes {
		errMsg, errFound, lookupErr := c.resolveMessageReference(path, errName)
		if lookupErr != nil {
			return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: lookupErr.Error()}
		}
		if !errFound {
			return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: fmt.Sprintf("unresolved error type %q", errName)}
		}
		errMsg.ErrorType = true
		status := 500
		if code, ok := errMsg.StatusCode(); ok {
			status = code
		}
		if previous, duplicate := seenStatuses[status]; duplicate {
			return nil, &Error{Path: path, Line: rd.Line, Column: rd.Col, Msg: fmt.Sprintf(
				"error types %q and %q use the same HTTP status %d", previous, errName, status,
			)}
		}
		seenStatuses[status] = errName
		method.ErrorTypes = append(method.ErrorTypes, errMsg)
	}

	return method, nil
}

func (c *compiler) buildHeaders(headers []onklang.HeaderDecl, path string) ([]*onkir.Header, error) {
	var out []*onkir.Header
	for _, h := range headers {
		kind, ok := onkir.ParseScalarKind(h.Type)
		if !ok {
			return nil, &Error{
				Path: path,
				Line: h.Line, Column: h.Col,
				Msg: fmt.Sprintf("invalid header type %q for %q", h.Type, h.Name),
			}
		}
		out = append(out, &onkir.Header{
			Name:       h.Name,
			Type:       kind,
			Decorators: convertDecorators(h.Decorators),
		})
	}
	return out, nil
}

func convertDecorators(decorators []onklang.Decorator) []onkir.Decorator {
	var out []onkir.Decorator
	for _, d := range decorators {
		out = append(out, onkir.Decorator{Name: d.Name, Args: convertArgs(d.Args)})
	}
	return out
}

func convertArgs(args []onklang.Arg) []onkir.Arg {
	var out []onkir.Arg
	for _, a := range args {
		out = append(out, onkir.Arg{Name: a.Name, Value: a.Value, Quoted: a.Quoted})
	}
	return out
}
