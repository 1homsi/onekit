// Package onkmigrate converts .onk schemas to TypeSpec. Every .onk construct
// maps onto TypeSpec and the @onkt/typespec decorators so that the emitter
// reproduces the same declarations, and therefore the same generated code.
package onkmigrate

import (
	"fmt"
	"math/big"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/1homsi/onekit/internal/onkschema"
)

// Source is one parsed .onk file.
type Source struct {
	Path string
	AST  *onkschema.File
}

// Output is one TypeSpec file the conversion writes.
type Output struct {
	Path    string
	Content string
}

// Options tunes a conversion.
type Options struct {
	// BasePath returns the base path a service without base_path is served
	// at, given its source file; .onk infers it from the directory, while
	// TypeSpec routes are always explicit.
	BasePath func(sourcePath string) string
	// Lenient writes a type name that resolves to nothing as it is spelled,
	// for converting documentation snippets that use undeclared types.
	Lenient bool
}

// EntryFile is the TypeSpec file every project compiles from.
const EntryFile = "main.tsp"

// Convert translates every source into a .tsp file next to it, and returns
// them in path order. Type references are resolved the way the .onk compiler
// resolves them and written fully qualified across namespaces.
func Convert(sources []Source, options Options) ([]Output, error) {
	c := newConverter(sources, options)
	c.prepare()
	outputs := make([]Output, 0, len(sources))
	for _, src := range sources {
		content, err := c.file(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.Path, err)
		}
		outputs = append(outputs, Output{Path: TSPPath(src.Path), Content: content})
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].Path < outputs[j].Path })
	return outputs, nil
}

// TSPPath is the TypeSpec file a .onk (or .proto) schema converts to.
func TSPPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".tsp"
}

// Entry returns main.tsp for root: it imports every converted file. When one
// of the converted files is itself root/main.tsp, its content gains the
// imports instead, and Entry reports false.
func Entry(root string, outputs []Output) (Output, bool) {
	entryPath := filepath.Join(root, EntryFile)
	var imports []string
	existing := -1
	for i, out := range outputs {
		if out.Path == entryPath {
			existing = i
			continue
		}
		rel, err := filepath.Rel(root, out.Path)
		if err != nil {
			rel = out.Path
		}
		imports = append(imports, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(imports)
	var b strings.Builder
	for _, path := range imports {
		fmt.Fprintf(&b, "import %s;\n", quote(path))
	}
	if existing >= 0 {
		return Output{Path: entryPath, Content: b.String() + outputs[existing].Content}, false
	}
	return Output{Path: entryPath, Content: b.String()}, true
}

// ------------------------------------------------------------- resolution

type decl struct {
	pkg     string
	name    string
	dir     string
	enum    bool
	message *onkschema.MessageDecl
}

type converter struct {
	sources []Source
	options Options

	byDir     map[string]map[string]*decl
	byName    map[string][]*decl
	byPkg     map[string]map[string]*decl
	msgDecl   map[*onkschema.MessageDecl]*decl
	errorMsgs map[*onkschema.MessageDecl]bool
	// bodyFields records the request field an RPC's @body("field") names.
	bodyFields map[*onkschema.MessageDecl]string
	// pathParams records the route parameters each request message fills.
	pathParams map[*onkschema.MessageDecl]map[string]bool
	// taken holds every TypeSpec name declared per namespace.
	taken map[string]map[string]bool
	// unionNames assigns each oneof field its union declaration.
	unionNames map[*onkschema.FieldDecl]string
}

func newConverter(sources []Source, options Options) *converter {
	return &converter{
		sources:    sources,
		options:    options,
		byDir:      map[string]map[string]*decl{},
		byName:     map[string][]*decl{},
		byPkg:      map[string]map[string]*decl{},
		msgDecl:    map[*onkschema.MessageDecl]*decl{},
		errorMsgs:  map[*onkschema.MessageDecl]bool{},
		bodyFields: map[*onkschema.MessageDecl]string{},
		pathParams: map[*onkschema.MessageDecl]map[string]bool{},
		taken:      map[string]map[string]bool{},
		unionNames: map[*onkschema.FieldDecl]string{},
	}
}

func (c *converter) add(d *decl) {
	if c.byDir[d.dir] == nil {
		c.byDir[d.dir] = map[string]*decl{}
	}
	c.byDir[d.dir][d.name] = d
	c.byName[d.name] = append(c.byName[d.name], d)
	if c.byPkg[d.pkg] == nil {
		c.byPkg[d.pkg] = map[string]*decl{}
	}
	if _, exists := c.byPkg[d.pkg][d.name]; !exists {
		c.byPkg[d.pkg][d.name] = d
	}
	c.take(d.pkg, d.name)
}

func (c *converter) take(pkg, name string) {
	if c.taken[pkg] == nil {
		c.taken[pkg] = map[string]bool{}
	}
	c.taken[pkg][name] = true
}

func (c *converter) prepare() {
	c.indexDeclarations()
	c.indexRPCs()
	c.nameUnions()
}

func (c *converter) indexDeclarations() {
	for _, src := range c.sources {
		dir := filepath.Dir(src.Path)
		var walk func(m *onkschema.MessageDecl)
		walk = func(m *onkschema.MessageDecl) {
			d := &decl{pkg: src.AST.Package, name: m.Name, dir: dir, message: m}
			c.add(d)
			c.msgDecl[m] = d
			for _, n := range m.Nested {
				walk(n)
			}
			for _, e := range m.NestedEn {
				c.add(&decl{pkg: src.AST.Package, name: e.Name, dir: dir, enum: true})
			}
		}
		for _, m := range src.AST.Messages {
			walk(m)
		}
		for _, e := range src.AST.Enums {
			c.add(&decl{pkg: src.AST.Package, name: e.Name, dir: dir, enum: true})
		}
		for _, s := range src.AST.Services {
			c.take(src.AST.Package, s.Name)
		}
	}
}

// indexRPCs records which messages are errors, @body requests and route
// parameter carriers.
func (c *converter) indexRPCs() {
	for _, src := range c.sources {
		for _, s := range src.AST.Services {
			for _, rpc := range s.RPCs {
				for _, name := range rpc.ErrorTypes {
					if d, err := c.resolve(src, name); err == nil && d.message != nil {
						c.errorMsgs[d.message] = true
					}
				}
				request, err := c.resolve(src, rpc.RequestType)
				if err != nil || request.message == nil {
					continue
				}
				if body, ok := findDecorator(rpc.Decorators, "body"); ok && len(body.Args) == 1 {
					c.bodyFields[request.message] = body.Args[0].Value
				}
				for _, d := range rpc.Decorators {
					if len(d.Args) == 0 || !routeDecorators[d.Name] {
						continue
					}
					if c.pathParams[request.message] == nil {
						c.pathParams[request.message] = map[string]bool{}
					}
					for _, param := range routeParameters(d.Args[0].Value) {
						c.pathParams[request.message][param] = true
					}
				}
			}
		}
	}
}

// nameUnions gives every oneof field a union declaration name that is free in
// its namespace.
func (c *converter) nameUnions() {
	for _, src := range c.sources {
		var walk func(m *onkschema.MessageDecl)
		walk = func(m *onkschema.MessageDecl) {
			for _, f := range m.Fields {
				if f.Oneof == nil {
					continue
				}
				base := m.Name + pascal(f.Name)
				name := base
				for i := 2; c.taken[src.AST.Package][name]; i++ {
					name = fmt.Sprintf("%sOneof%d", base, i)
					if i == 2 {
						name = base + "Oneof"
					}
				}
				c.take(src.AST.Package, name)
				c.unionNames[f] = name
			}
			for _, n := range m.Nested {
				walk(n)
			}
		}
		for _, m := range src.AST.Messages {
			walk(m)
		}
	}
}

// resolve finds the declaration a type name refers to from src: a
// package-qualified name, else the source's directory, else the one
// declaration of that name in the project.
func (c *converter) resolve(src Source, name string) (*decl, error) {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		if d := c.byPkg[name[:dot]][name[dot+1:]]; d != nil {
			return d, nil
		}
		return nil, fmt.Errorf("unresolved type %q", name)
	}
	if d := c.byDir[filepath.Dir(src.Path)][name]; d != nil {
		return d, nil
	}
	switch matches := c.byName[name]; len(matches) {
	case 0:
		if c.options.Lenient {
			return &decl{pkg: src.AST.Package, name: name}, nil
		}
		return nil, fmt.Errorf("unresolved type %q", name)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("type %q is declared in several directories; qualify it with its package", name)
	}
}

// usedNamespaces lists the other namespaces a file refers to, sorted.
func (c *converter) usedNamespaces(src Source) []string {
	seen := map[string]bool{}
	note := func(name string) {
		if _, scalar := scalarTypes[name]; scalar {
			return
		}
		if d, err := c.resolve(src, name); err == nil && d.pkg != "" && d.pkg != src.AST.Package {
			seen[d.pkg] = true
		}
	}
	var typeRef func(t *onkschema.TypeRef)
	typeRef = func(t *onkschema.TypeRef) {
		if t == nil {
			return
		}
		if t.IsMap {
			typeRef(t.MapVal)
			return
		}
		note(t.Name)
	}
	var message func(m *onkschema.MessageDecl)
	message = func(m *onkschema.MessageDecl) {
		for _, f := range m.Fields {
			typeRef(f.Type)
			if f.Oneof != nil {
				for _, v := range f.Oneof.Variants {
					typeRef(v.Type)
				}
			}
		}
		for _, n := range m.Nested {
			message(n)
		}
	}
	for _, m := range src.AST.Messages {
		message(m)
	}
	for _, s := range src.AST.Services {
		for _, rpc := range s.RPCs {
			note(rpc.RequestType)
			note(rpc.ResponseType)
			for _, e := range rpc.ErrorTypes {
				note(e)
			}
		}
	}
	pkgs := make([]string, 0, len(seen))
	for pkg := range seen {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	return pkgs
}

// shortNames maps each name a file may write unqualified to the namespace
// it means.
func (c *converter) shortNames(pkg string, used []string) map[string]string {
	owners := map[string][]string{}
	for _, p := range append([]string{pkg}, used...) {
		for name := range c.taken[p] {
			owners[name] = append(owners[name], p)
		}
	}
	short := map[string]string{}
	for name, pkgs := range owners {
		switch {
		case httpNames[name]:
		case len(pkgs) == 1:
			short[name] = pkgs[0]
		case pkgs[0] == pkg:
			// The file's own namespace wins over the ones it uses.
			short[name] = pkg
		}
	}
	return short
}

// ref spells a declaration as seen from a file: unqualified when the name is
// unambiguous there, otherwise fully qualified.
func (c *converter) refIn(fc *fileContext, d *decl) string {
	if fc.short != nil && fc.short[d.name] == d.pkg && d.pkg != "" {
		return ident(d.name)
	}
	return c.ref(fc.pkg, d)
}

// ref spells a declaration as seen from package pkg. Names TypeSpec.Http
// also declares are always qualified, since every file uses that namespace.
func (c *converter) ref(pkg string, d *decl) string {
	switch {
	case httpNames[d.name] && d.pkg == "":
		return "global." + ident(d.name)
	case httpNames[d.name], d.pkg != pkg && d.pkg != "":
		return namespaceName(d.pkg) + "." + ident(d.name)
	}
	return ident(d.name)
}

// httpNames are the declarations of TypeSpec.Http.
var httpNames = wordSet(`AcceptedResponse ApiKeyAuth ApiKeyLocation ApplyMergePatchOptions
		AuthType AuthorizationCodeFlow BadRequestResponse BasicAuth BearerAuth Body ClientCredentialsFlow
		ConflictResponse CookieOptions CreatedResponse File ForbiddenResponse HeaderOptions HttpPart
		HttpPartOptions ImplicitFlow Link LinkHeader LocationHeader MergePatchCreateOrUpdate MergePatchUpdate
		MergePatchVisibilityMode MovedResponse NoAuth NoContentResponse NotFoundResponse NotModifiedResponse
		OAuth OkResponse OpenIdConnectAuth PasswordFlow PatchOptions PathOptions PlainData QueryOptions
		Response UnauthorizedResponse`)

// --------------------------------------------------------------- rendering

type writer struct {
	b      strings.Builder
	indent int
}

func (w *writer) linef(format string, args ...any) {
	if format == "" {
		w.b.WriteString("\n")
		return
	}
	w.b.WriteString(strings.Repeat("  ", w.indent))
	fmt.Fprintf(&w.b, format, args...)
	w.b.WriteString("\n")
}

func (w *writer) comments(lines []string) {
	for _, comment := range lines {
		for _, line := range strings.Split(comment, "\n") {
			w.linef("%s", strings.TrimRight(line, " \t"))
		}
	}
}

func (w *writer) doc(doc string) {
	if doc == "" {
		return
	}
	lines := strings.Split(doc, "\n")
	if !plainDoc(lines) {
		// A doc comment loses leading indentation and cannot hold "*/";
		// @doc keeps the text exactly.
		w.linef("@doc(%s)", quote(doc))
		return
	}
	if len(lines) == 1 {
		w.linef("/** %s */", lines[0])
		return
	}
	w.linef("/**")
	for _, line := range lines {
		if line == "" {
			w.linef(" *")
			continue
		}
		w.linef(" * %s", line)
	}
	w.linef(" */")
}

// plainDoc reports whether doc text survives a /** */ comment unchanged.
func plainDoc(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "*/") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
			strings.HasPrefix(strings.TrimSpace(line), "@") || strings.HasSuffix(line, " ") {
			return false
		}
	}
	return strings.TrimSpace(lines[0]) != "" && strings.TrimSpace(lines[len(lines)-1]) != ""
}

type fileContext struct {
	src Source
	pkg string
	w   *writer
	// short holds the names this file can write unqualified: declared in
	// its own namespace or in exactly one namespace it uses.
	short map[string]string
}

func (c *converter) file(src Source) (string, error) {
	fc := &fileContext{src: src, pkg: src.AST.Package, w: &writer{}}
	used := c.usedNamespaces(src)
	fc.short = c.shortNames(fc.pkg, used)
	w := fc.w
	w.comments(src.AST.LeadingComments)
	w.linef(`import "@typespec/http";`)
	w.linef(`import "@onkt/typespec";`)
	w.linef("")
	w.linef("using TypeSpec.Http;")
	for _, pkg := range used {
		w.linef("using %s;", namespaceName(pkg))
	}
	w.linef("")
	w.doc(src.AST.PackageDoc)
	if fc.pkg != "" {
		w.linef("namespace %s;", namespaceName(fc.pkg))
	}

	decls := src.AST.Declarations
	if len(decls) == 0 {
		for _, m := range src.AST.Messages {
			decls = append(decls, m)
		}
		for _, e := range src.AST.Enums {
			decls = append(decls, e)
		}
		for _, s := range src.AST.Services {
			decls = append(decls, s)
		}
	}
	for _, d := range decls {
		var err error
		switch d := d.(type) {
		case *onkschema.MessageDecl:
			err = c.message(fc, d, nil)
		case *onkschema.EnumDecl:
			c.enum(fc, d, nil)
		case *onkschema.ServiceDecl:
			err = c.service(fc, d)
		}
		if err != nil {
			return "", err
		}
	}
	w.comments(src.AST.TrailingComments)
	return w.b.String(), nil
}

func (c *converter) message(fc *fileContext, m *onkschema.MessageDecl, parent *onkschema.MessageDecl) error {
	w := fc.w
	for _, f := range m.Fields {
		if f.Oneof != nil {
			if err := c.union(fc, m, f); err != nil {
				return err
			}
		}
	}
	w.linef("")
	w.comments(m.LeadingComments)
	w.doc(m.Doc)
	status := ""
	for _, d := range m.Decorators {
		switch d.Name {
		case "status":
			status = d.Args[0].Value
		case "rule":
			w.linef("@Onkt.rule(%s, %s)", quote(d.Args[0].Value), quote(d.Args[1].Value))
		case "principal":
			w.linef("@Onkt.principal")
		default:
			return fmt.Errorf("message %s: unsupported decorator @%s", m.Name, d.Name)
		}
	}
	if status != "" || c.errorMsgs[m] {
		w.linef("@error")
	}
	if parent != nil {
		w.linef("@Onkt.parent(%s)", c.refIn(fc, c.msgDecl[parent]))
	}
	if len(m.Fields) == 0 && status == "" && len(m.TrailingComments) == 0 {
		w.linef("model %s {}", ident(m.Name))
	} else {
		w.linef("model %s {", ident(m.Name))
		w.indent++
		if status != "" {
			w.linef("@statusCode _: %s;", status)
		}
		for _, f := range m.Fields {
			if err := c.field(fc, m, f); err != nil {
				return err
			}
		}
		w.comments(m.TrailingComments)
		w.indent--
		w.linef("}")
	}
	for _, nested := range m.Nested {
		if err := c.message(fc, nested, m); err != nil {
			return err
		}
	}
	for _, nested := range m.NestedEn {
		c.enum(fc, nested, m)
	}
	return nil
}

const deprecatedDecorator = "deprecated"

var integerScalars = map[string]bool{"int32": true, "int64": true, "uint32": true, "uint64": true}

func (c *converter) field(fc *fileContext, owner *onkschema.MessageDecl, f *onkschema.FieldDecl) error {
	w := fc.w
	w.comments(f.LeadingComments)
	w.doc(f.Doc)
	var decorators []string
	directive := ""
	nullable := false
	var literals []string
	encode := ""
	for _, d := range f.Decorators {
		arg := func(i int) string { return d.Args[i].Value }
		switch d.Name {
		case "email", "uuid", "uri":
			decorators = append(decorators, fmt.Sprintf("@format(%s)", quote(d.Name)))
		case "len":
			decorators = append(decorators, fmt.Sprintf("@minLength(%s)", number(arg(0))), fmt.Sprintf("@maxLength(%s)", number(arg(1))))
		case "range":
			decorators = append(decorators, fmt.Sprintf("@Onkt.range(%s, %s)", number(arg(0)), number(arg(1))))
		case "gt":
			decorators = append(decorators, fmt.Sprintf("@minValueExclusive(%s)", number(arg(0))))
		case "gte":
			decorators = append(decorators, fmt.Sprintf("@minValue(%s)", number(arg(0))))
		case "lt":
			decorators = append(decorators, fmt.Sprintf("@maxValueExclusive(%s)", number(arg(0))))
		case "lte":
			decorators = append(decorators, fmt.Sprintf("@maxValue(%s)", number(arg(0))))
		case "min_items":
			decorators = append(decorators, fmt.Sprintf("@minItems(%s)", number(arg(0))))
		case "max_items":
			decorators = append(decorators, fmt.Sprintf("@maxItems(%s)", number(arg(0))))
		case "pattern":
			decorators = append(decorators, fmt.Sprintf("@pattern(%s)", quote(arg(0))))
		case "in":
			if f.Type != nil && integerScalars[f.Type.Name] {
				values := make([]string, len(d.Args))
				for i := range d.Args {
					values[i] = number(arg(i))
				}
				decorators = append(decorators, fmt.Sprintf("@Onkt.values(%s)", strings.Join(values, ", ")))
				continue
			}
			if hasStringConstraint(f.Decorators) {
				values := make([]string, len(d.Args))
				for i := range d.Args {
					values[i] = quote(arg(i))
				}
				decorators = append(decorators, fmt.Sprintf("@Onkt.values(%s)", strings.Join(values, ", ")))
				continue
			}
			for i := range d.Args {
				literals = append(literals, quote(arg(i)))
			}
		case "required":
			decorators = append(decorators, "@Onkt.required")
		case "nullable":
			nullable = true
		case "unwrap", "object", "raw":
			decorators = append(decorators, "@Onkt."+d.Name)
		case "ws_id":
			decorators = append(decorators, "@Onkt.wsId")
		case "ws_timeout":
			decorators = append(decorators, "@Onkt.wsTimeout")
		case "flatten":
			if len(d.Args) == 1 {
				decorators = append(decorators, fmt.Sprintf("@Onkt.flatten(%s)", quote(arg(0))))
			} else {
				decorators = append(decorators, "@Onkt.flatten")
			}
		case "empty":
			decorators = append(decorators, fmt.Sprintf("@Onkt.empty(%s)", quote(arg(0))))
		case "encode":
			encode = arg(0)
		case "query":
			if len(d.Args) == 1 {
				decorators = append(decorators, fmt.Sprintf("@query(%s)", quote(arg(0))))
			} else {
				decorators = append(decorators, "@query")
			}
		case deprecatedDecorator:
			directive = deprecated(d)
		case "rule":
			decorators = append(decorators, fmt.Sprintf("@Onkt.rule(%s, %s)", quote(arg(0)), quote(arg(1))))
		default:
			return fmt.Errorf("field %s.%s: unsupported decorator @%s", owner.Name, f.Name, d.Name)
		}
	}
	if body, ok := c.bodyFields[owner]; ok {
		switch {
		case body == f.Name:
			decorators = append(decorators, "@body")
		case !c.pathParams[owner][f.Name] && !hasDecoratorNamed(f.Decorators, "query"):
			// Beside a @body field a request field that is neither a path
			// nor a query parameter does not cross the wire; TypeSpec
			// would read it as a second body.
			decorators = append(decorators, "@visibility(Lifecycle.Read)")
		}
	}

	var typ string
	if f.Oneof != nil {
		typ = ident(c.unionNames[f])
	} else {
		var extra []string
		var err error
		typ, extra, err = c.fieldType(fc, f, encode, literals)
		if err != nil {
			return fmt.Errorf("field %s.%s: %w", owner.Name, f.Name, err)
		}
		decorators = append(decorators, extra...)
	}
	if nullable {
		typ += " | null"
	}
	if directive != "" {
		w.linef("%s", directive)
	}
	for _, d := range decorators {
		w.linef("%s", d)
	}
	optional := ""
	if f.Optional {
		optional = "?"
	}
	w.linef("%s%s: %s;", ident(f.Name), optional, typ)
	return nil
}

// fieldType spells a field's type and returns the decorators its encoding
// needs.
func (c *converter) fieldType(fc *fileContext, f *onkschema.FieldDecl, encode string, literals []string) (string, []string, error) {
	t := f.Type
	var decorators []string
	if t.IsMap {
		if t.MapKey != "string" {
			decorators = append(decorators, fmt.Sprintf("@Onkt.mapKey(%s)", quote(t.MapKey)))
		}
		value, err := c.typeExpr(fc, t.MapVal)
		if err != nil {
			return "", nil, err
		}
		if encode != "" {
			decorators = append(decorators, fmt.Sprintf("@Onkt.encode(%s)", quote(encode)))
		}
		return "Record<" + value + ">", decorators, nil
	}
	if len(literals) > 0 {
		return strings.Join(literals, " | "), nil, nil
	}
	name, err := c.typeExpr(fc, t)
	if err != nil {
		return "", nil, err
	}
	switch {
	case (t.Name == "int64" || t.Name == "uint64") && f.Repeated:
		// TypeSpec's int64[] is numbers; .onk's follows int64_encoding.
		decorators = append(decorators, `@Onkt.encode("string")`)
	case (t.Name == "int64" || t.Name == "uint64") && !f.Repeated:
		switch encode {
		case "number":
		case "":
			decorators = append(decorators, "@encode(string)")
		default:
			decorators = append(decorators, fmt.Sprintf("@Onkt.encode(%s)", quote(encode)))
		}
	case t.Name == "timestamp" && encode == "date":
		name = "plainDate"
	case t.Name == "timestamp" && encode == "unix_seconds":
		decorators = append(decorators, `@encode("unixTimestamp", int64)`)
	case t.Name == "bytes" && encode == "base64url":
		decorators = append(decorators, `@encode("base64url")`)
	case encode != "":
		decorators = append(decorators, fmt.Sprintf("@Onkt.encode(%s)", quote(encode)))
	}
	if f.Repeated {
		name += "[]"
	}
	return name, decorators, nil
}

var scalarTypes = map[string]string{
	"string": "string", "bool": "boolean",
	"int32": "int32", "int64": "int64", "uint32": "uint32", "uint64": "uint64",
	"float32": "float32", "float64": "float64",
	"bytes": "bytes", "timestamp": "utcDateTime", "json": "unknown",
}

func (c *converter) typeExpr(fc *fileContext, t *onkschema.TypeRef) (string, error) {
	if t.IsMap {
		value, err := c.typeExpr(fc, t.MapVal)
		if err != nil {
			return "", err
		}
		if t.MapKey != "string" {
			return "", fmt.Errorf("a nested map with %s keys has no TypeSpec form", t.MapKey)
		}
		return "Record<" + value + ">", nil
	}
	if scalar, ok := scalarTypes[t.Name]; ok {
		return scalar, nil
	}
	d, err := c.resolve(fc.src, t.Name)
	if err != nil {
		return "", err
	}
	return c.refIn(fc, d), nil
}

func (c *converter) union(fc *fileContext, m *onkschema.MessageDecl, f *onkschema.FieldDecl) error {
	w := fc.w
	w.linef("")
	discriminator, flatten := "", false
	for _, arg := range f.Oneof.Args {
		switch arg.Name {
		case "discriminator":
			discriminator = arg.Value
		case "flatten":
			flatten = arg.Value == "true"
		}
	}
	switch {
	case flatten:
		if discriminator == "" {
			discriminator = "type"
		}
		w.linef("@discriminated(#{ discriminatorPropertyName: %s, envelope: \"none\" })", quote(discriminator))
	case discriminator != "":
		w.linef("@Onkt.discriminator(%s)", quote(discriminator))
	}
	w.linef("union %s {", ident(c.unionNames[f]))
	w.indent++
	for _, v := range f.Oneof.Variants {
		w.comments(v.LeadingComments)
		var decorators []string
		for _, d := range v.Decorators {
			switch d.Name {
			case "tag":
				decorators = append(decorators, fmt.Sprintf("@Onkt.tag(%s)", quote(d.Args[0].Value)))
			case "json":
				decorators = append(decorators, fmt.Sprintf("@Onkt.json(%s)", quote(d.Args[0].Value)))
			case "ws_cancel":
				decorators = append(decorators, "@Onkt.wsCancel")
			default:
				return fmt.Errorf("oneof %s.%s: unsupported variant decorator @%s", m.Name, f.Name, d.Name)
			}
		}
		typ, err := c.typeExpr(fc, v.Type)
		if err != nil {
			return fmt.Errorf("oneof %s.%s: %w", m.Name, f.Name, err)
		}
		prefix := ""
		if len(decorators) > 0 {
			prefix = strings.Join(decorators, " ") + " "
		}
		w.linef("%s%s: %s,", prefix, ident(v.Name), typ)
	}
	w.comments(f.Oneof.TrailingComments)
	w.indent--
	w.linef("}")
	return nil
}

func (c *converter) enum(fc *fileContext, e *onkschema.EnumDecl, parent *onkschema.MessageDecl) {
	w := fc.w
	w.linef("")
	w.comments(e.LeadingComments)
	w.doc(e.Doc)
	if parent != nil {
		w.linef("@Onkt.parent(%s)", c.refIn(fc, c.msgDecl[parent]))
	}
	w.linef("enum %s {", ident(e.Name))
	w.indent++
	for _, v := range e.Values {
		w.comments(v.LeadingComments)
		w.doc(v.Doc)
		value := ""
		for _, d := range v.Decorators {
			if d.Name == "json" {
				value = ": " + quote(d.Args[0].Value)
			}
		}
		w.linef("%s%s,", ident(v.Name), value)
	}
	w.comments(e.TrailingComments)
	w.indent--
	w.linef("}")
}

func (c *converter) service(fc *fileContext, s *onkschema.ServiceDecl) error {
	w := fc.w
	basePath := s.BasePath
	if basePath == "" && c.options.BasePath != nil {
		basePath = c.options.BasePath(fc.src.Path)
	}

	headersAlias := ""
	if len(s.Headers) > 0 {
		headersAlias = s.Name + "Headers"
		for i := 2; c.taken[fc.pkg][headersAlias]; i++ {
			headersAlias = fmt.Sprintf("%sHeaders%d", s.Name, i)
		}
		c.take(fc.pkg, headersAlias)
		w.linef("")
		w.comments(s.HeadersComments)
		w.linef("/** Headers every %s operation takes. */", s.Name)
		w.linef("alias %s = {", ident(headersAlias))
		w.indent++
		for _, h := range s.Headers {
			if err := c.header(fc, h); err != nil {
				return err
			}
		}
		w.comments(s.HeadersTrailingComments)
		w.indent--
		w.linef("};")
	}

	w.linef("")
	w.comments(s.LeadingComments)
	w.doc(s.Doc)
	w.comments(s.BasePathComments)
	if basePath != "" && basePath != "/" {
		w.linef("@route(%s)", quote(basePath))
	}
	w.linef("interface %s {", ident(s.Name))
	w.indent++
	for i, rpc := range s.RPCs {
		if i > 0 {
			w.linef("")
		}
		if err := c.rpc(fc, s, rpc, headersAlias); err != nil {
			return err
		}
	}
	w.comments(s.TrailingComments)
	w.indent--
	w.linef("}")
	return nil
}

func (c *converter) header(fc *fileContext, h onkschema.HeaderDecl) error {
	w := fc.w
	w.comments(h.LeadingComments)
	optional := "?"
	directive := ""
	decorators := []string{fmt.Sprintf("@header(%s)", quote(h.Name))}
	auth, schemeName := "", ""
	for _, d := range h.Decorators {
		switch d.Name {
		case "required":
			optional = ""
		case "format":
			decorators = append(decorators, fmt.Sprintf("@format(%s)", quote(d.Args[0].Value)))
		case "example":
			decorators = append(decorators, fmt.Sprintf("@Onkt.example(%s)", quote(d.Args[0].Value)))
		case deprecatedDecorator:
			directive = deprecated(d)
		case "auth":
			auth = d.Args[0].Value
		case "auth_scheme_name":
			schemeName = d.Args[0].Value
		default:
			return fmt.Errorf("header %s: unsupported decorator @%s", h.Name, d.Name)
		}
	}
	if auth != "" {
		if schemeName != "" {
			decorators = append(decorators, fmt.Sprintf("@Onkt.auth(%s, %s)", quote(auth), quote(schemeName)))
		} else {
			decorators = append(decorators, fmt.Sprintf("@Onkt.auth(%s)", quote(auth)))
		}
	}
	if directive != "" {
		w.linef("%s", directive)
	}
	w.linef("%s %s%s: string;", strings.Join(decorators, " "), headerParam(h.Name), optional)
	return nil
}

func (c *converter) rpc(fc *fileContext, s *onkschema.ServiceDecl, rpc *onkschema.RPCDecl, headersAlias string) error {
	w := fc.w
	w.comments(rpc.LeadingComments)
	w.doc(rpc.Doc)
	directive := ""
	var decorators []string
	success := ""
	for _, d := range rpc.Decorators {
		arg := func(i int) string { return d.Args[i].Value }
		switch d.Name {
		case "get", "post", "put", "patch", "delete":
			decorators = append(decorators, "@"+d.Name)
			if arg(0) != "" {
				decorators = append(decorators, fmt.Sprintf("@route(%s)", quote(route(arg(0)))))
			}
		case "any", "query":
			decorators = append(decorators, fmt.Sprintf("@Onkt.method(%s)", quote(strings.ToUpper(d.Name))))
			if arg(0) != "" {
				decorators = append(decorators, fmt.Sprintf("@route(%s)", quote(route(arg(0)))))
			}
		case "ws":
			decorators = append(decorators, "@Onkt.ws", fmt.Sprintf("@route(%s)", quote(route(arg(0)))))
		case "body":
			// The request field carries @body.
		case "success":
			success = arg(0)
		case "stream":
			decorators = append(decorators, "@Onkt.stream")
		case deprecatedDecorator:
			directive = deprecated(d)
		case "authorize":
			decorators = append(decorators, fmt.Sprintf("@Onkt.authorize(%s, %s)", quote(arg(0)), quote(arg(1))))
		case "meta":
			decorators = append(decorators, fmt.Sprintf("@Onkt.meta(%s, %s)", quote(arg(0)), quote(arg(1))))
		case "requires", "guard":
			values := make([]string, len(d.Args))
			for i := range d.Args {
				values[i] = quote(arg(i))
			}
			decorators = append(decorators, fmt.Sprintf("@Onkt.%s(%s)", d.Name, strings.Join(values, ", ")))
		case "max_body":
			decorators = append(decorators, fmt.Sprintf("@Onkt.maxBody(%s)", quote(arg(0))))
		case "http":
			var options []string
			for _, a := range d.Args {
				switch a.Name {
				case "":
					options = append(options, "contentType: "+quote(a.Value))
				default:
					options = append(options, a.Name+": "+quote(a.Value))
				}
			}
			if len(options) == 0 {
				decorators = append(decorators, "@Onkt.http")
			} else {
				decorators = append(decorators, fmt.Sprintf("@Onkt.http(#{ %s })", strings.Join(options, ", ")))
			}
		default:
			return fmt.Errorf("rpc %s.%s: unsupported decorator @%s", s.Name, rpc.Name, d.Name)
		}
	}

	request, err := c.resolve(fc.src, rpc.RequestType)
	if err != nil {
		return fmt.Errorf("rpc %s.%s: %w", s.Name, rpc.Name, err)
	}
	response, err := c.resolve(fc.src, rpc.ResponseType)
	if err != nil {
		return fmt.Errorf("rpc %s.%s: %w", s.Name, rpc.Name, err)
	}
	result := c.refIn(fc, response)
	if success != "" {
		result = fmt.Sprintf("{ @statusCode _: %s; @body body: %s }", success, result)
	}
	alternatives := []string{result}
	for _, name := range rpc.ErrorTypes {
		d, err := c.resolve(fc.src, name)
		if err != nil {
			return fmt.Errorf("rpc %s.%s: %w", s.Name, rpc.Name, err)
		}
		alternatives = append(alternatives, c.refIn(fc, d))
	}
	result = strings.Join(alternatives, " | ")

	if directive != "" {
		w.linef("%s", directive)
	}
	for _, d := range decorators {
		w.linef("%s", d)
	}
	var params []string
	if headersAlias != "" {
		params = append(params, "..."+ident(headersAlias))
	}
	if len(rpc.Headers) > 0 {
		w.linef("%s(", ident(rpc.Name))
		w.indent++
		for _, p := range params {
			w.linef("%s,", p)
		}
		w.comments(rpc.HeadersComments)
		for _, h := range rpc.Headers {
			sub := &fileContext{src: fc.src, pkg: fc.pkg, w: &writer{}}
			if err := c.header(sub, h); err != nil {
				return err
			}
			for _, line := range strings.Split(strings.TrimRight(sub.w.b.String(), "\n"), "\n") {
				if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "#") {
					w.linef("%s", line)
					continue
				}
				w.linef("%s,", strings.TrimSuffix(line, ";"))
			}
		}
		w.comments(rpc.HeadersTrailingComments)
		w.linef("...%s,", c.refIn(fc, request))
		w.indent--
		w.linef("): %s;", result)
	} else {
		params = append(params, "..."+c.refIn(fc, request))
		w.linef("%s(%s): %s;", ident(rpc.Name), strings.Join(params, ", "), result)
	}
	w.comments(rpc.TrailingComments)
	return nil
}

// ------------------------------------------------------------------ helpers

func findDecorator(decorators []onkschema.Decorator, name string) (onkschema.Decorator, bool) {
	for _, d := range decorators {
		if d.Name == name {
			return d, true
		}
	}
	return onkschema.Decorator{}, false
}

func deprecated(d onkschema.Decorator) string {
	if len(d.Args) == 1 {
		return "#deprecated " + quote(d.Args[0].Value)
	}
	return `#deprecated ""`
}

// quote writes a TypeSpec string literal.
func quote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range len(value) {
		switch ch := value[i]; ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '$':
			if i+1 < len(value) && value[i+1] == '{' {
				b.WriteString(`\$`)
			} else {
				b.WriteByte('$')
			}
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// number spells a numeric decorator argument as a TypeSpec literal, which has
// no exponent form.
func number(value string) string {
	value = strings.TrimPrefix(value, "+")
	if !strings.ContainsAny(value, "eE") {
		return value
	}
	f, _, err := big.ParseFloat(value, 10, 256, big.ToNearestEven)
	if err != nil {
		return value
	}
	return f.Text('f', -1)
}

var routeDecorators = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "any": true, "query": true, "ws": true}

var wildcardParameter = regexp.MustCompile(`\{([A-Za-z0-9_]+)\.\.\.\}`)

// route spells onkt wildcard parameters (`{path...}`) as RFC 6570 reserved
// expansions (`{+path}`).
func route(path string) string {
	return wildcardParameter.ReplaceAllString(path, "{+$1}")
}

var routeParameter = regexp.MustCompile(`\{([A-Za-z0-9_]+)(?:\.\.\.)?\}`)

func routeParameters(path string) []string {
	var names []string
	for _, match := range routeParameter.FindAllStringSubmatch(path, -1) {
		names = append(names, match[1])
	}
	return names
}

func hasDecoratorNamed(decorators []onkschema.Decorator, name string) bool {
	_, ok := findDecorator(decorators, name)
	return ok
}

// hasStringConstraint reports whether a string field has a constraint
// TypeSpec cannot apply to a union of string literals.
func hasStringConstraint(decorators []onkschema.Decorator) bool {
	for _, d := range decorators {
		switch d.Name {
		case "len", "pattern", "email", "uuid", "uri":
			return true
		}
	}
	return false
}

var plainIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// keywords are TypeSpec's reserved words, which need backticks as names.
var keywords = wordSet(`
		alias augment dec enum extern false fn import init interface is model namespace never op
		scalar true union unknown using valueof typeof void const else extends if internal projection
		return statemachine macro package metadata env arg declare array struct record module mod sym
		context prop property scenario pub sub typeref trait this self super keyof with implements impl
		satisfies flag auto partial private public protected sealed local async`)

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(words) {
		set[word] = true
	}
	return set
}

// ident writes a name as a TypeSpec identifier, in backticks when needed.
func ident(name string) string {
	if plainIdentifier.MatchString(name) && !keywords[name] {
		return name
	}
	return "`" + strings.ReplaceAll(name, "`", "\\`") + "`"
}

func namespaceName(pkg string) string {
	segments := strings.Split(pkg, ".")
	for i, segment := range segments {
		segments[i] = ident(segment)
	}
	return strings.Join(segments, ".")
}

// headerParam names the operation parameter that carries a header.
func headerParam(header string) string {
	var b strings.Builder
	b.WriteString("header_")
	for _, r := range strings.ToLower(header) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func pascal(name string) string {
	var b strings.Builder
	upper := true
	for _, r := range name {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper {
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
