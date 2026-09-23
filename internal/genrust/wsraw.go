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

const rustWSRawRuntimeSource = `pub trait WsRawFrame: Sized {
const WS_RAW: bool;
fn ws_split_raw(self, raw: &mut Vec<Vec<u8>>) -> Self;
fn ws_join_raw(&mut self, raw: &mut std::vec::IntoIter<Vec<u8>>) -> bool;
const WS_TIMEOUT: bool = false;
fn ws_with_timeout(self, _ms: u64) -> Self { self }
}

pub const WS_CHUNK_BYTES: usize = 8 << 20;
pub const WS_CHUNK_THRESHOLD: usize = 16 << 20;
pub const DEFAULT_MAX_WS_MESSAGE_BYTES: usize = 256 << 20;

pub enum WsPiece<'a> {
Whole(&'a [u8]),
Assembled(bool, Vec<u8>),
Pending,
}

#[derive(Default)]
pub struct WsAssembler {
buf: Option<Vec<u8>>,
total: usize,
kind: u8,
}

impl WsAssembler {
pub fn active(&self) -> bool { self.buf.is_some() }

pub fn feed<'a>(&mut self, data: &'a [u8], limit: usize) -> Result<WsPiece<'a>, u16> {
if data.len() < 13 || data[0..4] != [0xff; 4] {
if self.buf.is_some() { return Err(1007); }
return Ok(WsPiece::Whole(data));
}
let kind = data[4];
let mut total_bytes = [0u8; 8];
total_bytes.copy_from_slice(&data[5..13]);
let total = u64::from_be_bytes(total_bytes) as usize;
let chunk = &data[13..];
if kind > 1 { return Err(1007); }
match self.buf.as_mut() {
None => {
if total > limit { return Err(1009); }
let mut buf = Vec::with_capacity(total);
buf.extend_from_slice(chunk);
self.buf = Some(buf);
self.total = total;
self.kind = kind;
}
Some(buf) => {
if total != self.total || kind != self.kind { return Err(1007); }
buf.extend_from_slice(chunk);
}
}
let filled = self.buf.as_ref().map_or(0, Vec::len);
if filled > self.total { return Err(1007); }
if filled < self.total { return Ok(WsPiece::Pending); }
Ok(WsPiece::Assembled(self.kind == 1, self.buf.take().unwrap_or_default()))
}
}

pub fn ws_chunks(binary: bool, data: &[u8]) -> Option<Vec<Vec<u8>>> {
if data.len() <= WS_CHUNK_THRESHOLD { return None; }
let mut header = [0u8; 13];
header[0..4].copy_from_slice(&[0xff; 4]);
header[4] = u8::from(binary);
header[5..13].copy_from_slice(&(data.len() as u64).to_be_bytes());
Some(data.chunks(WS_CHUNK_BYTES).map(|part| { let mut out = Vec::with_capacity(13 + part.len()); out.extend_from_slice(&header); out.extend_from_slice(part); out }).collect())
}

pub fn ws_encode_frame<T: WsRawFrame + Serialize>(value: T) -> Result<(bool, Vec<u8>), String> {
if !T::WS_RAW {
return serde_json::to_vec(&value).map(|data| (false, data)).map_err(|error| error.to_string());
}
let mut raw = Vec::new();
let header = value.ws_split_raw(&mut raw);
let header_json = serde_json::to_vec(&header).map_err(|error| error.to_string())?;
let size: usize = raw.iter().map(Vec::len).sum();
if size == 0 {
return Ok((false, header_json));
}
let mut out = Vec::with_capacity(8 + header_json.len() + 4 * raw.len() + size);
out.extend_from_slice(&(header_json.len() as u32).to_be_bytes());
out.extend_from_slice(&header_json);
out.extend_from_slice(&(raw.len() as u32).to_be_bytes());
for segment in &raw {
out.extend_from_slice(&(segment.len() as u32).to_be_bytes());
}
for segment in &raw {
out.extend_from_slice(segment);
}
Ok((true, out))
}

pub fn ws_decode_frame<T: WsRawFrame + serde::de::DeserializeOwned>(binary: bool, data: &[u8]) -> Result<T, String> {
if !binary || !T::WS_RAW {
return serde_json::from_slice(data).map_err(|error| error.to_string());
}
let malformed = || "malformed binary frame".to_string();
let read_u32 = |data: &[u8], at: usize| -> Option<usize> { data.get(at..at + 4).map(|b| u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as usize) };
let header_len = read_u32(data, 0).ok_or_else(malformed)?;
let mut offset = 4usize;
let header = data.get(offset..offset + header_len).ok_or_else(malformed)?;
offset += header_len;
let count = read_u32(data, offset).ok_or_else(malformed)?;
offset += 4;
let mut lengths = Vec::with_capacity(count.min(data.len() / 4));
for _ in 0..count {
lengths.push(read_u32(data, offset).ok_or_else(malformed)?);
offset += 4;
}
let mut raw = Vec::with_capacity(lengths.len());
for length in lengths {
raw.push(data.get(offset..offset + length).ok_or_else(malformed)?.to_vec());
offset += length;
}
if offset != data.len() {
return Err(malformed());
}
let mut value: T = serde_json::from_slice(header).map_err(|error| error.to_string())?;
let mut segments = raw.into_iter();
if !value.ws_join_raw(&mut segments) || segments.next().is_some() {
return Err(malformed());
}
Ok(value)
}`
