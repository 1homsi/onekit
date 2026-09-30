package gendart

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
	line := fmt.Sprint(args...)
	if line == "" {
		p.b.WriteByte('\n')
		return
	}
	p.b.WriteString(strings.Repeat("  ", p.indent))
	p.b.WriteString(line)
	p.b.WriteByte('\n')
}

func (p *Printer) Indent() { p.indent++ }

func (p *Printer) Dedent() {
	if p.indent > 0 {
		p.indent--
	}
}

func (p *Printer) Blank() { p.b.WriteByte('\n') }

func (p *Printer) Bytes() []byte { return []byte(p.b.String()) }

func (p *Printer) MessageTypeName(m *onkir.Message) string {
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveMessage(m); ok {
			return ref.Alias + "." + MessageName(m)
		}
	}
	return MessageName(m)
}

func (p *Printer) EnumTypeName(e *onkir.Enum) string {
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveEnum(e); ok {
			return ref.Alias + "." + EnumName(e)
		}
	}
	return EnumName(e)
}

func (p *Printer) isExternal(m *onkir.Message) bool {
	if p.resolver == nil {
		return false
	}
	_, ok := p.resolver.ResolveMessage(m)
	return ok
}
