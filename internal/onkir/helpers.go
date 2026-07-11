package onkir

import (
	"strconv"
	"strings"
)

var HTTPVerbs = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true, "patch": true, "query": true,
}

func FindDecorator(decorators []Decorator, name string) (Decorator, bool) {
	for _, d := range decorators {
		if d.Name == name {
			return d, true
		}
	}
	return Decorator{}, false
}

func HasDecorator(decorators []Decorator, name string) bool {
	_, ok := FindDecorator(decorators, name)
	return ok
}

func (d Decorator) Arg(i int) (string, bool) {
	if i < 0 || i >= len(d.Args) {
		return "", false
	}
	return d.Args[i].Value, true
}

func (d Decorator) NamedArg(name string) (string, bool) {
	for _, a := range d.Args {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

func (d Decorator) Value() (string, bool) {
	return d.Arg(0)
}

func (f *Field) Decorator(name string) (Decorator, bool) {
	return FindDecorator(f.Decorators, name)
}

func (f *Field) HasDecorator(name string) bool {
	return HasDecorator(f.Decorators, name)
}

func (o *Oneof) NamedArg(name string) (string, bool) {
	for _, a := range o.Args {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

func (o *Oneof) Discriminator() (string, bool) {
	return o.NamedArg("discriminator")
}

func (o *Oneof) Flatten() bool {
	v, ok := o.NamedArg("flatten")
	return ok && v == "true"
}

func (v *OneofVariant) Decorator(name string) (Decorator, bool) {
	return FindDecorator(v.Decorators, name)
}

func (v *OneofVariant) Tag() string {
	if d, ok := v.Decorator("tag"); ok {
		if val, ok := d.Value(); ok {
			return val
		}
	}
	return v.Name
}

func (h *Header) Decorator(name string) (Decorator, bool) {
	return FindDecorator(h.Decorators, name)
}

func (h *Header) Required() bool {
	return HasDecorator(h.Decorators, "required")
}

func (h *Header) Format() (string, bool) {
	if d, ok := h.Decorator("format"); ok {
		return d.Value()
	}
	return "", false
}

func (h *Header) Example() (string, bool) {
	if d, ok := h.Decorator("example"); ok {
		return d.Value()
	}
	return "", false
}

func (h *Header) Deprecated() (string, bool) {
	if d, ok := h.Decorator("deprecated"); ok {
		return d.Value()
	}
	return "", false
}

func (h *Header) AuthType() (string, bool) {
	if d, ok := h.Decorator("auth"); ok {
		return d.Value()
	}
	return "", false
}

func (h *Header) AuthSchemeName() (string, bool) {
	if d, ok := h.Decorator("auth_scheme_name"); ok {
		return d.Value()
	}
	return "", false
}

func (m *Method) Decorator(name string) (Decorator, bool) {
	return FindDecorator(m.Decorators, name)
}

func (m *Method) Verb() (string, bool) {
	for _, d := range m.Decorators {
		if HTTPVerbs[d.Name] {
			return d.Name, true
		}
	}
	return "", false
}

func (m *Method) Path() (string, bool) {
	verb, ok := m.Verb()
	if !ok {
		return "", false
	}
	d, _ := m.Decorator(verb)
	return d.Value()
}

func (m *Method) IsStream() bool {
	return m.HasDecorator("stream")
}

func (m *Method) HasDecorator(name string) bool {
	return HasDecorator(m.Decorators, name)
}

func (m *Method) BodyField() (string, bool) {
	if d, ok := m.Decorator("body"); ok {
		return d.Value()
	}
	return "", false
}

func (m *Message) IsError() bool {
	return strings.HasSuffix(m.Name, "Error")
}

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

func (m *Message) FullName() string {
	var parts []string
	for cur := m; cur != nil; cur = cur.Parent {
		parts = append([]string{cur.Name}, parts...)
	}
	if m.File != nil && m.File.Package != "" {
		return m.File.Package + "." + strings.Join(parts, ".")
	}
	return strings.Join(parts, ".")
}

func (e *Enum) FullName() string {
	var parts []string
	parts = append(parts, e.Name)
	for cur := e.Parent; cur != nil; cur = cur.Parent {
		parts = append([]string{cur.Name}, parts...)
	}
	if e.File != nil && e.File.Package != "" {
		return e.File.Package + "." + strings.Join(parts, ".")
	}
	return strings.Join(parts, ".")
}

func (v *EnumValue) JSONName() string {
	if d, ok := FindDecorator(v.Decorators, "json"); ok {
		if val, ok := d.Value(); ok {
			return val
		}
	}
	return v.Name
}
