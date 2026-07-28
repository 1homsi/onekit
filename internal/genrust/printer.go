package genrust

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type Printer struct {
	b        strings.Builder
	indent   int
	resolver PackageResolver
}

func newPrinter(resolver PackageResolver) *Printer {
	return &Printer{resolver: resolver}
}

func (p *Printer) P(args ...any) {
	for range p.indent {
		p.b.WriteString("    ")
	}
	for _, arg := range args {
		_, _ = fmt.Fprint(&p.b, arg)
	}
	p.b.WriteByte('\n')
}

func (p *Printer) Blank() {
	p.b.WriteByte('\n')
}

func (p *Printer) Indent() {
	p.indent++
}

func (p *Printer) Dedent() {
	if p.indent > 0 {
		p.indent--
	}
}

func (p *Printer) Bytes() []byte {
	return []byte(p.b.String())
}

func (p *Printer) MessageTypeName(message *onkir.Message) string {
	name := RustMessageName(message)
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveMessage(message); ok {
			return ref.ModulePath + "::" + name
		}
	}
	return name
}

func (p *Printer) EnumTypeName(enum *onkir.Enum) string {
	name := RustEnumName(enum)
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveEnum(enum); ok {
			return ref.ModulePath + "::" + name
		}
	}
	return name
}

func (p *Printer) RustType(t *onkir.Type) string {
	if t == nil {
		return "()"
	}
	switch t.Kind {
	case onkir.KindScalar:
		return RustScalarType(t.Scalar)
	case onkir.KindMessage:
		return p.MessageTypeName(t.Message)
	case onkir.KindEnum:
		return p.EnumTypeName(t.Enum)
	case onkir.KindMap:
		return "std::collections::HashMap<" + RustScalarType(t.MapKey) + ", " + p.RustType(t.MapValue) + ">"
	default:
		return "()"
	}
}
