package genswift

import (
	"fmt"
	"strings"
)

type Printer struct {
	b      strings.Builder
	indent int
}

func newPrinter() *Printer { return &Printer{} }

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
