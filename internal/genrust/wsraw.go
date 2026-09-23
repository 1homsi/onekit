package genrust

import (
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
	var out []*onkir.Message
	var walk func(ms []*onkir.Message)
	walk = func(ms []*onkir.Message) {
		for _, m := range ms {
			out = append(out, m)
			walk(m.Nested)
		}
	}
	walk(file.Messages)
	return out
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
	p.P("pub trait WsRawFrame: Sized {")
	p.P("const WS_RAW: bool;")
	p.P("fn ws_split_raw(self, raw: &mut Vec<Vec<u8>>) -> Self;")
	p.P("fn ws_join_raw(&mut self, raw: &mut std::vec::IntoIter<Vec<u8>>) -> bool;")
	p.P("const WS_TIMEOUT: bool = false;")
	p.P("fn ws_with_timeout(self, _ms: u64) -> Self { self }")
	p.P("}")
	p.Blank()
	p.P("pub fn ws_encode_frame<T: WsRawFrame + Serialize>(value: T) -> Result<(bool, Vec<u8>), String> {")
	p.P("if !T::WS_RAW {")
	p.P("return serde_json::to_vec(&value).map(|data| (false, data)).map_err(|error| error.to_string());")
	p.P("}")
	p.P("let mut raw = Vec::new();")
	p.P("let header = value.ws_split_raw(&mut raw);")
	p.P("let header_json = serde_json::to_vec(&header).map_err(|error| error.to_string())?;")
	p.P("let size: usize = raw.iter().map(Vec::len).sum();")
	p.P("if size == 0 {")
	p.P("return Ok((false, header_json));")
	p.P("}")
	p.P("let mut out = Vec::with_capacity(8 + header_json.len() + 4 * raw.len() + size);")
	p.P("out.extend_from_slice(&(header_json.len() as u32).to_be_bytes());")
	p.P("out.extend_from_slice(&header_json);")
	p.P("out.extend_from_slice(&(raw.len() as u32).to_be_bytes());")
	p.P("for segment in &raw {")
	p.P("out.extend_from_slice(&(segment.len() as u32).to_be_bytes());")
	p.P("}")
	p.P("for segment in &raw {")
	p.P("out.extend_from_slice(segment);")
	p.P("}")
	p.P("Ok((true, out))")
	p.P("}")
	p.Blank()
	p.P("pub fn ws_decode_frame<T: WsRawFrame + serde::de::DeserializeOwned>(binary: bool, data: &[u8]) -> Result<T, String> {")
	p.P("if !binary || !T::WS_RAW {")
	p.P("return serde_json::from_slice(data).map_err(|error| error.to_string());")
	p.P("}")
	p.P(`let malformed = || "malformed binary frame".to_string();`)
	p.P("let read_u32 = |data: &[u8], at: usize| -> Option<usize> { data.get(at..at + 4).map(|b| u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as usize) };")
	p.P("let header_len = read_u32(data, 0).ok_or_else(malformed)?;")
	p.P("let mut offset = 4usize;")
	p.P("let header = data.get(offset..offset + header_len).ok_or_else(malformed)?;")
	p.P("offset += header_len;")
	p.P("let count = read_u32(data, offset).ok_or_else(malformed)?;")
	p.P("offset += 4;")
	p.P("let mut lengths = Vec::with_capacity(count.min(data.len() / 4));")
	p.P("for _ in 0..count {")
	p.P("lengths.push(read_u32(data, offset).ok_or_else(malformed)?);")
	p.P("offset += 4;")
	p.P("}")
	p.P("let mut raw = Vec::with_capacity(lengths.len());")
	p.P("for length in lengths {")
	p.P("raw.push(data.get(offset..offset + length).ok_or_else(malformed)?.to_vec());")
	p.P("offset += length;")
	p.P("}")
	p.P("if offset != data.len() {")
	p.P("return Err(malformed());")
	p.P("}")
	p.P("let mut value: T = serde_json::from_slice(header).map_err(|error| error.to_string())?;")
	p.P("let mut segments = raw.into_iter();")
	p.P("if !value.ws_join_raw(&mut segments) || segments.next().is_some() {")
	p.P("return Err(malformed());")
	p.P("}")
	p.P("Ok(value)")
	p.P("}")
	p.Blank()
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
				p.P("Some(", oneof, "::", variant, "(value)) => Some(", oneof, "::", variant, "(value.ws_split_raw(raw))),")
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
