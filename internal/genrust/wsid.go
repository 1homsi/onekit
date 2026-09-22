package genrust

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

// writeWSCorrelatedImpls emits the WsCorrelated trait and one impl per
// distinct message type reachable from a @ws_id-using method's request or
// response, so client.rs and server.rs can both call frame.ws_id() on a
// concrete message type. This has to live in types.rs (shared by both)
// rather than being duplicated per-file: Rust's coherence rules forbid two
// `impl WsCorrelated<K> for SameType` blocks anywhere in one crate, even in
// separate modules, so generating it twice (once per generated file, the
// pattern used for Go's/TS's per-file runtime helpers) would not compile.
func writeWSCorrelatedImpls(p *Printer, file *onkir.File) {
	if !onkir.FileHasWSCorrelation(file) {
		return
	}
	p.P("// WsCorrelated lets a @ws_id-using method's frame type report its own")
	p.P("// correlation key, so the generated client/server read loops can route a")
	p.P("// reply to the pending Call() awaiting it instead of the ordinary handler.")
	p.P("pub trait WsCorrelated<K> {")
	p.Indent()
	p.P("fn ws_id(&self) -> Option<K>;")
	p.P("// ws_variant is the oneof variant tag of an id-bearing frame (\"\" when the")
	p.P("// id is a direct field): a call's reply must be a different variant.")
	p.P("fn ws_variant(&self) -> &'static str;")
	p.P("// ws_cancel builds the schema's @ws_cancel frame for id, if it declares one:")
	p.P("// what an abandoned call() sends so the peer can stop working on id.")
	p.P("fn ws_cancel(id: K) -> Option<Self> where Self: Sized;")
	p.Dedent()
	p.P("}")
	p.Blank()

	seen := map[string]bool{}
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !m.IsWebSocket() {
				continue
			}
			anyIDField, ok := m.WSIDField()
			if !ok {
				continue
			}
			kType := RustScalarType(anyIDField.Type.Scalar)
			for _, message := range []*onkir.Message{m.Request, m.Response} {
				if message == nil || seen[message.Name] {
					continue
				}
				seen[message.Name] = true
				localIDField, _ := onkir.WSIDField(message)
				writeWSCorrelatedImpl(p, message, kType, localIDField)
			}
		}
	}
}

func writeWSCorrelatedImpl(p *Printer, message *onkir.Message, kType string, idField *onkir.Field) {
	p.P("impl WsCorrelated<", kType, "> for ", message.Name, " {")
	p.Indent()
	p.P("fn ws_id(&self) -> Option<", kType, "> {")
	p.Indent()
	if idField == nil {
		p.P("None")
	} else {
		writeWSIDMatchBody(p, message)
	}
	p.Dedent()
	p.P("}")
	p.Blank()
	writeWSVariantFn(p, message)
	p.Blank()
	writeWSCancelFn(p, message, kType)
	p.Dedent()
	p.P("}")
	p.Blank()
}

func writeWSVariantFn(p *Printer, message *onkir.Message) {
	p.P("fn ws_variant(&self) -> &'static str {")
	p.Indent()
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		oneofType := OneofTypeName(message, f)
		for _, variant := range f.Oneof.Variants {
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
				continue
			}
			if _, ok := onkir.WSIDField(variant.Type.Message); !ok {
				continue
			}
			p.P("if let Some(", oneofType, "::", PascalCase(variant.Name), "(_)) = &self.", RustIdent(f.Name), " { return ", fmt.Sprintf("%q", variant.Tag()), "; }")
		}
	}
	p.P(`""`)
	p.Dedent()
	p.P("}")
}

func writeWSCancelFn(p *Printer, message *onkir.Message, kType string) {
	oneofField, variant, idField, ok := onkir.WSCancelVariant(message)
	if !ok {
		p.P("fn ws_cancel(_id: ", kType, ") -> Option<Self> { None }")
		return
	}
	idValue := "id"
	if idField.Optional {
		idValue = "Some(id)"
	}
	p.P("#[allow(clippy::needless_update)]")
	p.P("fn ws_cancel(id: ", kType, ") -> Option<Self> {")
	p.Indent()
	p.P(
		"Some(Self { ", RustIdent(oneofField.Name), ": Some(", OneofTypeName(message, oneofField), "::", PascalCase(variant.Name),
		"(", PascalCase(variant.Type.Message.Name), " { ", RustIdent(idField.Name), ": ", idValue, ", ..Default::default() })), ..Default::default() })",
	)
	p.Dedent()
	p.P("}")
}

// writeWSIDMatchBody emits statements returning Some(id) for whichever
// field of message actually carries @ws_id - a direct field, or
// (independently, per variant) any oneof variant whose own message carries
// one - falling through to a final `None`. Each variant has its own
// distinct field (e.g. HostCall.id vs HostResult.id) with its own name and
// optionality, found fresh per variant rather than filtered against a
// single reference field: comparing against one shared field's identity
// only ever matched whichever field onkir.WSIDField(message) happened to
// return first (declaration order), silently emitting no match arm at all
// for every other variant's @ws_id field.
func writeWSIDMatchBody(p *Printer, message *onkir.Message) {
	returnFound := func(field *onkir.Field, accessor string) {
		if field.Optional && field.Type.Kind != onkir.KindMessage {
			p.P("return ", accessor, ".clone();")
			return
		}
		p.P("return Some(", accessor, ".clone());")
	}
	for _, f := range message.Fields {
		if f.Oneof != nil {
			for _, variant := range f.Oneof.Variants {
				// A cancel is never a reply: it goes to the handler/receive().
				if variant.IsWSCancel() || variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
					continue
				}
				vf, ok := onkir.WSIDField(variant.Type.Message)
				if !ok {
					continue
				}
				oneofType := OneofTypeName(message, f)
				p.P("if let Some(", oneofType, "::", PascalCase(variant.Name), "(value)) = &self.", RustIdent(f.Name), " {")
				p.Indent()
				returnFound(vf, "value."+RustIdent(vf.Name))
				p.Dedent()
				p.P("}")
			}
			continue
		}
		if !f.HasDecorator("ws_id") {
			continue
		}
		returnFound(f, "self."+RustIdent(f.Name))
	}
	p.P("None")
}
