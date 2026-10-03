package gengo

import (
	"sort"

	"github.com/1homsi/onekit/internal/onkir"
)

// PackageRef identifies another generated Go package that a cross-package
// type reference needs to import.
type PackageRef struct {
	Alias      string
	ImportPath string
}

// PackageResolver tells the generator whether a message/enum belongs to a
// different generated Go package than the one currently being written, and
// if so, which package to import for it. A nil PackageResolver (the default
// used by GenerateTypes/GenerateServer/GenerateClient) treats every type as
// local, preserving single-package generation behavior.
type PackageResolver interface {
	ResolveMessage(m *onkir.Message) (PackageRef, bool)
	ResolveEnum(e *onkir.Enum) (PackageRef, bool)
}

// refCollector accumulates the distinct set of external packages referenced
// during a walk, in first-seen order (sorted by import path at the end).
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
		// scalars never reference another generated package
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
	sort.Slice(c.refs, func(i, j int) bool { return c.refs[i].ImportPath < c.refs[j].ImportPath })
	return c.refs
}

// collectExternalRefs walks every field type reachable from a file's own
// messages (the only thing types.go ever prints a type name for) and returns
// the distinct set of external packages referenced, sorted by import path.
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
// types of a file's services - the only types server.go/client.go ever print
// a type name for (they never drill into a message's own fields).
func collectServiceExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, s := range file.Services {
		for _, meth := range s.Methods {
			c.addMessage(meth.Request)
			c.addMessage(meth.Response)
			c.addMessage(meth.Principal)
			for _, errType := range meth.ErrorTypes {
				c.addMessage(errType)
			}
		}
	}
	return c.sorted()
}
