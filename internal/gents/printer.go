package gents

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

type Printer struct {
	b        strings.Builder
	resolver PackageResolver
}

func newPrinter(resolver PackageResolver) *Printer {
	return &Printer{resolver: resolver}
}

func (p *Printer) P(args ...any) {
	for _, a := range args {
		fmt.Fprint(&p.b, a)
	}
	p.b.WriteByte('\n')
}

func (p *Printer) Bytes() []byte {
	return []byte(p.b.String())
}

// MessageTypeName returns the TS type name to use when referencing m from
// the module currently being printed: the bare name if m belongs to this
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

// MessageCodecName returns the encode<Message>/decode<Message> function name
// to call for m, qualified with m's package alias if it belongs to a
// different generated module - e.g. "common.encodeMoney", not
// "encodecommon.Money" (which MessageTypeName's own qualified form would
// produce if the "encode"/"decode" prefix were naively prepended to it).
func (p *Printer) MessageCodecName(m *onkir.Message, prefix string) string {
	if p.resolver != nil {
		if ref, ok := p.resolver.ResolveMessage(m); ok {
			return ref.Alias + "." + prefix + m.Name
		}
	}
	return prefix + m.Name
}

// TSFieldType resolves Message/Enum kinds through this printer's
// PackageResolver so a cross-module field type gets an import-qualified name
// instead of a bare (and possibly wrong) local one.
func (p *Printer) TSFieldType(t *onkir.Type) string {
	switch t.Kind {
	case onkir.KindScalar:
		return TSScalarType(t.Scalar)
	case onkir.KindMessage:
		return p.MessageTypeName(t.Message)
	case onkir.KindEnum:
		return p.EnumTypeName(t.Enum)
	case onkir.KindMap:
		return "Record<string, " + p.TSFieldType(t.MapValue) + ">"
	default:
		return "unknown"
	}
}
