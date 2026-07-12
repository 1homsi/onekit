package genpy

import (
	"sort"

	"github.com/1homsi/onekit/internal/onkir"
)

// PackageRef identifies another generated Python module that a cross-package
// type reference needs to import.
type PackageRef struct {
	Alias      string // Python identifier used for the import alias and as the qualifier prefix
	ModulePath string // dotted Python module path, e.g. "common.models"
}

// PackageResolver tells the generator whether a message/enum belongs to a
// different generated Python module than the one currently being written,
// and if so, which module to import for it. A nil PackageResolver (the
// default) treats every type as local, preserving single-module generation.
type PackageResolver interface {
	ResolveMessage(m *onkir.Message) (PackageRef, bool)
	ResolveEnum(e *onkir.Enum) (PackageRef, bool)
}

type refCollector struct {
	resolver PackageResolver
	seen     map[PackageRef]bool
	refs     []PackageRef
}

func newRefCollector(resolver PackageResolver) *refCollector {
	return &refCollector{resolver: resolver, seen: map[PackageRef]bool{}}
}

func (c *refCollector) addMessage(m *onkir.Message) {
	if m == nil {
		return
	}
	if ref, ok := c.resolver.ResolveMessage(m); ok && !c.seen[ref] {
		c.seen[ref] = true
		c.refs = append(c.refs, ref)
	}
}

func (c *refCollector) addEnum(e *onkir.Enum) {
	if e == nil {
		return
	}
	if ref, ok := c.resolver.ResolveEnum(e); ok && !c.seen[ref] {
		c.seen[ref] = true
		c.refs = append(c.refs, ref)
	}
}

func (c *refCollector) addType(t *onkir.Type) {
	if t == nil {
		return
	}
	switch t.Kind {
	case onkir.KindMessage:
		c.addMessage(t.Message)
	case onkir.KindEnum:
		c.addEnum(t.Enum)
	case onkir.KindMap:
		c.addType(t.MapValue)
	case onkir.KindScalar:
		// scalars never reference another generated module
	}
}

func (c *refCollector) addMessageFields(m *onkir.Message) {
	for _, f := range m.Fields {
		if f.Oneof != nil {
			for _, v := range f.Oneof.Variants {
				c.addType(v.Type)
			}
			continue
		}
		c.addType(f.Type)
	}
	for _, nested := range m.Nested {
		c.addMessageFields(nested)
	}
	for _, nested := range m.NestedEnums {
		c.addEnum(nested)
	}
}

func (c *refCollector) sorted() []PackageRef {
	sort.Slice(c.refs, func(i, j int) bool { return c.refs[i].ModulePath < c.refs[j].ModulePath })
	return c.refs
}

// collectExternalRefs walks every field type reachable from a file's own
// messages/error classes (the only thing models.py ever prints a type name
// for) and returns the distinct set of external modules referenced, sorted
// by module path.
func collectExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, m := range file.Messages {
		c.addMessageFields(m)
	}
	return c.sorted()
}

// collectServiceExternalRefs walks only the top-level request/response/error
// types of a file's services - the only types client.py ever prints a type
// name for (it never drills into a message's own fields).
func collectServiceExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, s := range file.Services {
		for _, meth := range s.Methods {
			c.addMessage(meth.Request)
			c.addMessage(meth.Response)
			for _, errType := range meth.ErrorTypes {
				c.addMessage(errType)
			}
		}
	}
	return c.sorted()
}
