package genswift

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type PackageResolver interface {
	ResolveMessage(m *onkir.Message) (string, bool)
	ResolveEnum(e *onkir.Enum) (string, bool)
}

type Printer struct {
	b         strings.Builder
	indent    int
	resolver  PackageResolver
	rules     *swiftRuleState
	namespace string
}

func newPrinter(resolver PackageResolver) *Printer { return &Printer{resolver: resolver} }

func (p *Printer) messageRef(m *onkir.Message) string {
	name := MessageName(m)
	if p.resolver != nil {
		if namespace, ok := p.resolver.ResolveMessage(m); ok && namespace != "" {
			return namespace + "." + name
		}
	}
	return name
}

func (p *Printer) enumRef(e *onkir.Enum) string {
	name := EnumName(e)
	if p.resolver != nil {
		if namespace, ok := p.resolver.ResolveEnum(e); ok && namespace != "" {
			return namespace + "." + name
		}
	}
	return name
}

func (p *Printer) P(args ...any) {
	line := fmt.Sprint(args...)
	if line == "" {
		p.b.WriteByte('\n')
		return
	}
	p.b.WriteString(strings.Repeat("    ", p.indent))
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
