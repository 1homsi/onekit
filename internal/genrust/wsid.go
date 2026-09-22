package genrust

import (
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
		writeWSIDMatchBody(p, message, idField)
	}
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("}")
	p.Blank()
}

// writeWSIDMatchBody emits statements returning Some(id) as soon as idField
// is found - directly on message, or within whichever oneof variant's own
// message carries it - falling through to a final `None`.
func writeWSIDMatchBody(p *Printer, message *onkir.Message, idField *onkir.Field) {
	returnFound := func(accessor string) {
		if idField.Optional && idField.Type.Kind != onkir.KindMessage {
			p.P("return ", accessor, ".clone();")
			return
		}
		p.P("return Some(", accessor, ".clone());")
	}
	for _, f := range message.Fields {
		if f.Oneof != nil {
			for _, variant := range f.Oneof.Variants {
				if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
					continue
				}
				vf, ok := onkir.WSIDField(variant.Type.Message)
				if !ok || vf != idField {
					continue
				}
				oneofType := OneofTypeName(message, f)
				p.P("if let Some(", oneofType, "::", PascalCase(variant.Name), "(value)) = &self.", RustIdent(f.Name), " {")
				p.Indent()
				returnFound("value." + RustIdent(idField.Name))
				p.Dedent()
				p.P("}")
			}
			continue
		}
		if f != idField {
			continue
		}
		returnFound("self." + RustIdent(idField.Name))
	}
	p.P("None")
}
