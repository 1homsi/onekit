package onkir

import (
	"strconv"
	"strings"
)

// HTTPVerbs is the set of decorator names that bind an RPC to an HTTP verb.
var HTTPVerbs = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true, "patch": true, "query": true,
}

// FindDecorator returns the first decorator with the given name.
func FindDecorator(decorators []Decorator, name string) (Decorator, bool) {
	for _, d := range decorators {
		if d.Name == name {
			return d, true
		}
	}
	return Decorator{}, false
}

// HasDecorator reports whether any decorator carries the given name.
func HasDecorator(decorators []Decorator, name string) bool {
	_, ok := FindDecorator(decorators, name)
	return ok
}

// Arg returns the i-th positional argument value when present.
func (d Decorator) Arg(i int) (string, bool) {
	if i < 0 || i >= len(d.Args) {
		return "", false
	}
	return d.Args[i].Value, true
}

// NamedArg returns the value of the named argument when present.
func (d Decorator) NamedArg(name string) (string, bool) {
	for _, a := range d.Args {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// Value returns the first positional argument value when present.
func (d Decorator) Value() (string, bool) {
	return d.Arg(0)
}

// Decorator finds one of the field's decorators by name.
func (f *Field) Decorator(name string) (Decorator, bool) {
	return FindDecorator(f.Decorators, name)
}

// HasDecorator reports whether the field carries the named decorator.
func (f *Field) HasDecorator(name string) bool {
	return HasDecorator(f.Decorators, name)
}

// NamedArg returns the value of the named oneof argument when present.
func (o *Oneof) NamedArg(name string) (string, bool) {
	for _, a := range o.Args {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// Discriminator returns the oneof's discriminator field name.
func (o *Oneof) Discriminator() (string, bool) {
	return o.NamedArg("discriminator")
}

// Flatten reports whether the oneof is flattened into its parent object.
func (o *Oneof) Flatten() bool {
	v, ok := o.NamedArg("flatten")
	return ok && v == "true"
}

// Decorator finds one of the variant's decorators by name.
func (v *OneofVariant) Decorator(name string) (Decorator, bool) {
	return FindDecorator(v.Decorators, name)
}

// Tag returns the variant's wire tag value, defaulting to its name.
func (v *OneofVariant) Tag() string {
	if d, ok := v.Decorator("tag"); ok {
		if val, ok := d.Value(); ok {
			return val
		}
	}
	return v.Name
}

// Decorator finds one of the header's decorators by name.
func (h *Header) Decorator(name string) (Decorator, bool) {
	return FindDecorator(h.Decorators, name)
}

// Required reports whether the header must be present on every request.
func (h *Header) Required() bool {
	return HasDecorator(h.Decorators, "required")
}

// Format returns the header's declared format constraint (uuid/email/uri).
func (h *Header) Format() (string, bool) {
	if d, ok := h.Decorator("format"); ok {
		return d.Value()
	}
	return "", false
}

// Example returns the header's documented example value.
func (h *Header) Example() (string, bool) {
	if d, ok := h.Decorator("example"); ok {
		return d.Value()
	}
	return "", false
}

// Deprecated returns the header's deprecation notice, if any.
func (h *Header) Deprecated() (string, bool) {
	if d, ok := h.Decorator("deprecated"); ok {
		return d.Value()
	}
	return "", false
}

// AuthType returns the header's auth scheme (api_key/bearer/basic).
func (h *Header) AuthType() (string, bool) {
	if d, ok := h.Decorator("auth"); ok {
		return d.Value()
	}
	return "", false
}

// AuthSchemeName returns the OpenAPI security scheme override for the header.
func (h *Header) AuthSchemeName() (string, bool) {
	if d, ok := h.Decorator("auth_scheme_name"); ok {
		return d.Value()
	}
	return "", false
}

// Decorator finds one of the method's decorators by name.
func (m *Method) Decorator(name string) (Decorator, bool) {
	return FindDecorator(m.Decorators, name)
}

// Verb returns the HTTP verb the method is bound to.
func (m *Method) Verb() (string, bool) {
	for _, d := range m.Decorators {
		if HTTPVerbs[d.Name] {
			return d.Name, true
		}
	}
	return "", false
}

// Path returns the route path from the method's HTTP verb decorator.
func (m *Method) Path() (string, bool) {
	verb, ok := m.Verb()
	if !ok {
		return "", false
	}
	d, _ := m.Decorator(verb)
	return d.Value()
}

// IsStream reports whether the method streams its response over SSE.
func (m *Method) IsStream() bool {
	return m.HasDecorator("stream")
}

// HasDecorator reports whether the method carries the named decorator.
func (m *Method) HasDecorator(name string) bool {
	return HasDecorator(m.Decorators, name)
}

// BodyField returns the request field bound as the body via @body.
func (m *Method) BodyField() (string, bool) {
	if d, ok := m.Decorator("body"); ok {
		return d.Value()
	}
	return "", false
}

// IsError reports whether the message participates in an error union either
// explicitly (via ErrorType) or by naming convention.
func (m *Message) IsError() bool {
	return m.ErrorType || strings.HasSuffix(m.Name, "Error")
}

// StatusCode returns the @status error status declared on the message.
func (m *Message) StatusCode() (int, bool) {
	d, ok := FindDecorator(m.Decorators, "status")
	if !ok {
		return 0, false
	}
	v, ok := d.Value()
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// qualifiedName joins leaf with the names of every enclosing message
// (nearest first), prefixed by the file's package when present. Ancestor
// names are collected forward and reversed in place instead of prepending to
// keep assembly linear.
func qualifiedName(leaf string, parent *Message, file *File) string {
	parts := make([]string, 0, 4)
	parts = append(parts, leaf)
	for cur := parent; cur != nil; cur = cur.Parent {
		parts = append(parts, cur.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	joined := strings.Join(parts, ".")
	if file != nil && file.Package != "" {
		return file.Package + "." + joined
	}
	return joined
}

// FullName returns the schema-qualified name of the message, honoring an
// explicit SchemaName override.
func (m *Message) FullName() string {
	if m.SchemaName != "" {
		return m.SchemaName
	}
	return qualifiedName(m.Name, m.Parent, m.File)
}

// FullName returns the schema-qualified name of the enum, honoring an
// explicit SchemaName override.
func (e *Enum) FullName() string {
	if e.SchemaName != "" {
		return e.SchemaName
	}
	return qualifiedName(e.Name, e.Parent, e.File)
}

// JSONName returns the enum value's wire spelling, honoring @json overrides.
func (v *EnumValue) JSONName() string {
	if d, ok := FindDecorator(v.Decorators, "json"); ok {
		if val, ok := d.Value(); ok {
			return val
		}
	}
	return v.Name
}

// FindField returns msg's field with the given name, or nil when absent.
func FindField(msg *Message, name string) *Field {
	if msg == nil {
		return nil
	}
	for _, f := range msg.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// PathParamNames extracts the `{name}` route parameter names from a path by
// scanning brace-delimited segments. All generator backends share this one
// implementation so route matching stays consistent across languages.
func PathParamNames(path string) []string {
	var names []string
	start := -1
	for i, c := range path {
		if c == '{' {
			start = i + 1
		} else if c == '}' && start >= 0 {
			names = append(names, path[start:i])
			start = -1
		}
	}
	return names
}

// IsBodyBearingVerb reports whether the HTTP verb carries a request body.
func IsBodyBearingVerb(verb string) bool {
	return verb == "post" || verb == "put" || verb == "patch" || verb == "query"
}

// FileHasStreamMethods reports whether any service in the file declares an
// SSE streaming RPC.
func FileHasStreamMethods(file *File) bool {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsStream() {
				return true
			}
		}
	}
	return false
}
