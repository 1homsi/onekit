package genrust

import (
	_ "embed"

	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

func (p *Printer) isExternalMessage(m *onkir.Message) bool {
	if p.resolver == nil {
		return false
	}
	_, ok := p.resolver.ResolveMessage(m)
	return ok
}

func fileMessagesDeep(file *onkir.File) []*onkir.Message {
	return genshared.FileMessagesDeep(file)
}

func writeWSRawImpls(p *Printer, file *onkir.File) {
	if !onkir.FileHasWSMethods(file) {
		return
	}
	targets := map[*onkir.Message]bool{}
	var order []*onkir.Message
	add := func(m *onkir.Message) {
		if m == nil || targets[m] || p.isExternalMessage(m) {
			return
		}
		targets[m] = true
		order = append(order, m)
	}
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsWebSocket() {
				add(m.Request)
				add(m.Response)
			}
		}
	}
	for _, m := range fileMessagesDeep(file) {
		if onkir.MessageHasRaw(m, p.isExternalMessage) {
			add(m)
		}
	}
	writeWSRawRuntime(p)
	for _, m := range order {
		writeWSRawImpl(p, m)
	}
}

func writeWSRawRuntime(p *Printer) {
	p.P(rustWSRawRuntimeSource)
}

func isRawString(f *onkir.Field) bool {
	return f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarString
}

func writeWSRawImpl(p *Printer, m *onkir.Message) {
	name := RustMessageName(m)
	hasRaw := onkir.MessageHasRaw(m, p.isExternalMessage)
	steps := onkir.RawSteps(m, p.isExternalMessage)
	p.P("impl WsRawFrame for ", name, " {")
	writeWSTimeoutImpl(p, m)
	if !hasRaw {
		p.P("const WS_RAW: bool = false;")
		p.P("fn ws_split_raw(self, _raw: &mut Vec<Vec<u8>>) -> Self { self }")
		p.P("fn ws_join_raw(&mut self, _raw: &mut std::vec::IntoIter<Vec<u8>>) -> bool { true }")
		p.P("}")
		p.Blank()
		return
	}
	p.P("const WS_RAW: bool = true;")
	p.P("fn ws_split_raw(mut self, raw: &mut Vec<Vec<u8>>) -> Self {")
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		field := "self." + RustIdent(step.Field.Name)
		switch {
		case step.Variant != nil:
			oneof := OneofTypeName(m, step.Field)
			p.P(field, " = match ", field, " {")
			for ; i < len(steps) && steps[i].Field == step.Field; i++ {
				variant := PascalCase(steps[i].Variant.Name)
				split := "value.ws_split_raw(raw)"
				if boxedVariant(m, steps[i].Variant) {
					split = "Box::new((*value).ws_split_raw(raw))"
				}
				p.P("Some(", oneof, "::", variant, "(value)) => Some(", oneof, "::", variant, "(", split, ")),")
			}
			i--
			p.P("other => other,")
			p.P("};")
		case step.Child != nil && step.Field.Repeated:
			p.P(field, " = ", field, ".into_iter().map(|item| item.ws_split_raw(raw)).collect();")
		case step.Child != nil:
			p.P(field, " = ", field, ".map(|item| Box::new((*item).ws_split_raw(raw)));")
		case isRawString(step.Field):
			p.P("raw.push(std::mem::take(&mut ", field, ").into_bytes());")
		default:
			p.P("raw.push(std::mem::take(&mut ", field, "));")
		}
	}
	p.P("self")
	p.P("}")
	p.Blank()
	p.P("fn ws_join_raw(&mut self, raw: &mut std::vec::IntoIter<Vec<u8>>) -> bool {")
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		field := "self." + RustIdent(step.Field.Name)
		switch {
		case step.Variant != nil:
			oneof := OneofTypeName(m, step.Field)
			for ; i < len(steps) && steps[i].Field == step.Field; i++ {
				variant := PascalCase(steps[i].Variant.Name)
				p.P("if let Some(", oneof, "::", variant, "(value)) = ", field, ".as_mut() {")
				p.P("if !value.ws_join_raw(raw) { return false; }")
				p.P("}")
			}
			i--
		case step.Child != nil && step.Field.Repeated:
			p.P("for item in ", field, ".iter_mut() {")
			p.P("if !item.ws_join_raw(raw) { return false; }")
			p.P("}")
		case step.Child != nil:
			p.P("if let Some(item) = ", field, ".as_mut() {")
			p.P("if !item.ws_join_raw(raw) { return false; }")
			p.P("}")
		case isRawString(step.Field):
			p.P("let Some(segment) = raw.next() else { return false; };")
			p.P("let Ok(text) = String::from_utf8(segment) else { return false; };")
			p.P(field, " = text;")
		default:
			p.P("let Some(segment) = raw.next() else { return false; };")
			p.P(field, " = segment;")
		}
	}
	p.P("true")
	p.P("}")
	p.P("}")
	p.Blank()
}

func writeWSTimeoutImpl(p *Printer, m *onkir.Message) {
	if !onkir.MessageHasWSTimeout(m) {
		return
	}
	p.P("const WS_TIMEOUT: bool = true;")
	p.P("fn ws_with_timeout(mut self, ms: u64) -> Self {")
	if f := onkir.WSTimeoutField(m); f != nil {
		field := "self." + RustIdent(f.Name)
		p.P("if ", field, " == 0 { ", field, " = ms as ", RustScalarType(f.Type.Scalar), "; }")
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			tf := onkir.WSTimeoutField(v.Type.Message)
			if tf == nil {
				continue
			}
			p.P("if let Some(", OneofTypeName(m, f), "::", PascalCase(v.Name), "(inner)) = self.", RustIdent(f.Name), ".as_mut() {")
			p.P("if inner.", RustIdent(tf.Name), " == 0 { inner.", RustIdent(tf.Name), " = ms as ", RustScalarType(tf.Type.Scalar), "; }")
			p.P("}")
		}
	}
	p.P("self")
	p.P("}")
}

//go:embed runtime/ws_raw.rs
var rustWSRawRuntimeSource string
