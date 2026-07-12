package genpy

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
	p.b.WriteString(strings.Repeat("    ", p.indent))
	for _, a := range args {
		fmt.Fprint(&p.b, a)
	}
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

func (p *Printer) Blank() {
	p.b.WriteByte('\n')
}

func (p *Printer) Bytes() []byte {
	return []byte(p.b.String())
}

// MessageTypeName returns the Python name to use when referencing m from the
// module currently being printed: the bare class name if m belongs to this
// same generated module, or an import-qualified name (e.g. "common.Money")
// if it belongs to a different one (see PackageResolver).
func (p *Printer) MessageTypeName(m *onkir.Message) string {
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveMessage(m); ok {
			return ref.Alias + "." + m.Name
		}
	}
	return m.Name
}

// EnumTypeName is MessageTypeName's counterpart for enums.
func (p *Printer) EnumTypeName(e *onkir.Enum) string {
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveEnum(e); ok {
			return ref.Alias + "." + e.Name
		}
	}
	return e.Name
}
