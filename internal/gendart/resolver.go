package gendart

import (
	"sort"

	"github.com/1homsi/onekit/internal/onkir"
)

type PackageRef struct {
	Alias      string
	ImportPath string
}

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

func (c *refCollector) add(ref PackageRef, ok bool) {
	if ok && !c.seen[ref] {
		c.seen[ref] = true
		c.refs = append(c.refs, ref)
	}
}

func (c *refCollector) addMessage(m *onkir.Message) {
	if m != nil {
		c.add(c.resolver.ResolveMessage(m))
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
		c.add(c.resolver.ResolveEnum(t.Enum))
	case onkir.KindMap:
		c.addType(t.MapValue)
	case onkir.KindScalar:
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
}

func (c *refCollector) sorted() []PackageRef {
	sort.Slice(c.refs, func(i, j int) bool { return c.refs[i].ImportPath < c.refs[j].ImportPath })
	return c.refs
}

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

func collectServiceExternalRefs(file *onkir.File, resolver PackageResolver) []PackageRef {
	if resolver == nil {
		return nil
	}
	c := newRefCollector(resolver)
	for _, s := range file.Services {
		for _, m := range s.Methods {
			c.addMessage(m.Request)
			c.addMessage(m.Response)
			for _, e := range m.ErrorTypes {
				c.addMessage(e)
			}
		}
	}
	return c.sorted()
}
