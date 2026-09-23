package gents

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

func isRawBytes(f *onkir.Field) bool {
	return f != nil && f.IsRaw() && f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarBytes
}

func (p *Printer) isExternalMessage(m *onkir.Message) bool {
	return !isLocalMessage(p.resolver, m)
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

func fileHasRawBytes(file *onkir.File) bool {
	for _, m := range fileMessagesDeep(file) {
		for _, f := range m.Fields {
			if isRawBytes(f) {
				return true
			}
		}
	}
	return false
}

func (p *Printer) rawSplitName(m *onkir.Message) string {
	return p.MessageCodecName(m, "split") + "Raw"
}

func (p *Printer) rawJoinName(m *onkir.Message) string {
	return p.MessageCodecName(m, "join") + "Raw"
}

func (p *Printer) wsRawCodecArgs(m *onkir.Message) (string, string) {
	if !onkir.MessageHasRaw(m, p.isExternalMessage) {
		return "undefined", "undefined"
	}
	return p.rawSplitName(m), p.rawJoinName(m)
}

func wsRawImportNames(p *Printer, file *onkir.File, frames func(*onkir.Method) []*onkir.Message) []string {
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !m.IsWebSocket() {
				continue
			}
			add("wsEncodeMessage")
			add("wsDecodeMessage")
			add("wsSend")
			add("WSAssembler")
			add("WSChunkError")
			add("DEFAULT_MAX_WS_MESSAGE_BYTES")
			for _, frame := range frames(m) {
				if _, correlated := m.WSIDField(); correlated && onkir.MessageHasWSTimeout(frame) && !p.isExternalMessage(frame) {
					add(p.timeoutFnName(frame))
				}
				if onkir.MessageHasRaw(frame, p.isExternalMessage) {
					add(p.rawSplitName(frame))
					add(p.rawJoinName(frame))
				}
			}
		}
	}
	return names
}

func writeTSBase64Helpers(p *Printer) {
	p.P("function wsBase64Encode(bytes: Uint8Array): string {")
	p.P(`let text = "";`)
	p.P("for (let i = 0; i < bytes.length; i += 0x8000) text += String.fromCharCode(...bytes.subarray(i, i + 0x8000));")
	p.P("return btoa(text);")
	p.P("}")
	p.P()
	p.P("function wsBase64Decode(text: string): Uint8Array {")
	p.P("const binary = atob(text);")
	p.P("const out = new Uint8Array(binary.length);")
	p.P("for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);")
	p.P("return out;")
	p.P("}")
	p.P()
}

func writeTSWSCodecRuntime(p *Printer) {
	p.P(tsWSCodecRuntimeSource)
}

func writeTSRawFuncs(p *Printer, m *onkir.Message) {
	steps := onkir.RawSteps(m, p.isExternalMessage)
	typeName := p.MessageTypeName(m)
	p.P("export function ", p.rawSplitName(m), "(v: ", typeName, ", raw: (Uint8Array | string)[]): ", typeName, " {")
	p.P("const c = { ...v };")
	for _, step := range steps {
		prop := "c." + CamelCase(step.Field.Name)
		switch {
		case step.Variant != nil:
			disc := oneofDiscriminatorKey(step.Field)
			split := p.rawSplitName(step.Child)
			if step.Field.Oneof.Flatten() {
				p.P(fmt.Sprintf("if (%s && %s.%s === %q) %s = { ...%s(%s, raw), %s: %q };", prop, prop, disc, step.Variant.Tag(), prop, split, prop, disc, step.Variant.Tag()))
				continue
			}
			vProp := CamelCase(step.Variant.Name)
			p.P(fmt.Sprintf("if (%s && %s.%s === %q && %s.%s !== undefined) %s = { ...%s, %s: %s(%s.%s, raw) };", prop, prop, disc, step.Variant.Tag(), prop, vProp, prop, prop, vProp, split, prop, vProp))
		case step.Child != nil && step.Field.Repeated:
			p.P("if (", prop, " !== undefined) ", prop, " = ", prop, ".map((item) => ", p.rawSplitName(step.Child), "(item, raw));")
		case step.Child != nil:
			p.P("if (", prop, " !== undefined) ", prop, " = ", p.rawSplitName(step.Child), "(", prop, ", raw);")
		case isRawBytes(step.Field):
			p.P("raw.push(", prop, " ?? new Uint8Array(0));")
			p.P(prop, " = new Uint8Array(0);")
		default:
			p.P("raw.push(", prop, ` ?? "");`)
			p.P(prop, ` = "";`)
		}
	}
	p.P("return c;")
	p.P("}")
	p.P()
	p.P("export function ", p.rawJoinName(m), "(v: ", typeName, ", raw: Uint8Array[], at: { i: number }): boolean {")
	for _, step := range steps {
		prop := "v." + CamelCase(step.Field.Name)
		switch {
		case step.Variant != nil:
			disc := oneofDiscriminatorKey(step.Field)
			join := p.rawJoinName(step.Child)
			target := prop
			if !step.Field.Oneof.Flatten() {
				target = prop + "." + CamelCase(step.Variant.Name)
			}
			p.P(fmt.Sprintf("if (%s && %s.%s === %q && %s !== undefined && !%s(%s, raw, at)) return false;", prop, prop, disc, step.Variant.Tag(), target, join, target))
		case step.Child != nil && step.Field.Repeated:
			p.P("for (const item of ", prop, " ?? []) if (!", p.rawJoinName(step.Child), "(item, raw, at)) return false;")
		case step.Child != nil:
			p.P("if (", prop, " !== undefined && !", p.rawJoinName(step.Child), "(", prop, ", raw, at)) return false;")
		default:
			p.P("{")
			p.P("const segment = raw[at.i++];")
			p.P("if (segment === undefined) return false;")
			if isRawBytes(step.Field) {
				p.P(prop, " = segment;")
			} else {
				p.P(prop, " = wsTextDecoder.decode(segment);")
			}
			p.P("}")
		}
	}
	p.P("return true;")
	p.P("}")
	p.P()
}

func (p *Printer) timeoutFnName(m *onkir.Message) string {
	return p.MessageCodecName(m, "with") + "Timeout"
}

func tsTimeoutValue(p *Printer, f *onkir.Field) string {
	if p.scalarWireTSType(f) == tsTypeString {
		return "String(Math.ceil(ms))"
	}
	return "Math.ceil(ms)"
}

func writeTSTimeoutFunc(p *Printer, m *onkir.Message) {
	typeName := p.MessageTypeName(m)
	p.P("export function ", p.timeoutFnName(m), "(v: ", typeName, ", ms: number): ", typeName, " {")
	p.P("if (!(ms > 0)) return v;")
	p.P("const c = { ...v };")
	if f := onkir.WSTimeoutField(m); f != nil {
		prop := "c." + CamelCase(f.Name)
		p.P("if (!", prop, " || ", prop, ` === "0") `, prop, " = ", tsTimeoutValue(p, f), ";")
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		disc := oneofDiscriminatorKey(f)
		prop := "c." + CamelCase(f.Name)
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			tf := onkir.WSTimeoutField(v.Type.Message)
			if tf == nil {
				continue
			}
			inner := prop + "." + CamelCase(v.Name)
			if f.Oneof.Flatten() {
				inner = prop
			}
			tprop := inner + "." + CamelCase(tf.Name)
			value := tsTimeoutValue(p, tf)
			if f.Oneof.Flatten() {
				p.P(fmt.Sprintf("if (%s && %s.%s === %q && (!%s || %s === \"0\")) %s = { ...%s, %s: %s };", prop, prop, disc, v.Tag(), tprop, tprop, prop, prop, CamelCase(tf.Name), value))
				continue
			}
			p.P(fmt.Sprintf("if (%s && %s.%s === %q && %s && (!%s || %s === \"0\")) %s = { ...%s, %s: { ...%s, %s: %s } };", prop, prop, disc, v.Tag(), inner, tprop, tprop, prop, prop, CamelCase(v.Name), inner, CamelCase(tf.Name), value))
		}
	}
	p.P("return c;")
	p.P("}")
	p.P()
}

const tsWSCodecRuntimeSource = `const wsTextEncoder = new TextEncoder();
const wsTextDecoder = new TextDecoder();

function wsMalformedFrame(): Error {
return new Error("malformed binary frame");
}

function wsBytes(data: unknown): Uint8Array {
if (data instanceof Uint8Array) return data;
if (data instanceof ArrayBuffer) return new Uint8Array(data);
if (ArrayBuffer.isView(data)) return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
throw wsMalformedFrame();
}

export function wsEncodeRawFrame(header: string, raw: (Uint8Array | string)[]): ArrayBuffer {
const head = wsTextEncoder.encode(header);
let size = 0;
for (const segment of raw) size += typeof segment === "string" ? segment.length : segment.byteLength;
const out = new Uint8Array(8 + head.byteLength + 4 * raw.length + size);
const view = new DataView(out.buffer);
view.setUint32(0, head.byteLength);
out.set(head, 4);
let offset = 4 + head.byteLength;
view.setUint32(offset, raw.length);
offset += 4;
let data = offset + 4 * raw.length;
for (const segment of raw) {
if (typeof segment === "string") {
const written = wsTextEncoder.encodeInto(segment, out.subarray(data, data + segment.length));
if (written.read !== segment.length) return wsEncodeRawFrame(header, raw.map((s) => typeof s === "string" ? wsTextEncoder.encode(s) : s));
view.setUint32(offset, written.written);
data += written.written;
} else {
view.setUint32(offset, segment.byteLength);
out.set(segment, data);
data += segment.byteLength;
}
offset += 4;
}
return out.buffer;
}

export function wsDecodeRawFrame(data: Uint8Array): { header: string; raw: Uint8Array[] } {
const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
if (data.byteLength < 4) throw wsMalformedFrame();
const headerLen = view.getUint32(0);
let offset = 4;
if (offset + headerLen + 4 > data.byteLength) throw wsMalformedFrame();
const header = wsTextDecoder.decode(data.subarray(offset, offset + headerLen));
offset += headerLen;
const count = view.getUint32(offset);
offset += 4;
if (offset + count * 4 > data.byteLength) throw wsMalformedFrame();
const lengths: number[] = [];
for (let i = 0; i < count; i++) { lengths.push(view.getUint32(offset)); offset += 4; }
const raw: Uint8Array[] = [];
for (const n of lengths) {
if (offset + n > data.byteLength) throw wsMalformedFrame();
raw.push(data.subarray(offset, offset + n));
offset += n;
}
if (offset !== data.byteLength) throw wsMalformedFrame();
return { header, raw };
}

export const DEFAULT_MAX_WS_MESSAGE_BYTES = 256 * 1024 * 1024;
const WS_CHUNK_BYTES = 8 * 1024 * 1024;
const WS_CHUNK_THRESHOLD = 16 * 1024 * 1024;

export class WSChunkError extends Error {
constructor(readonly code: number, message: string) { super(message); this.name = "WSChunkError"; }
}

export class WSAssembler {
private buf: Uint8Array | undefined = undefined;
private filled = 0;
private kind = 0;
constructor(private limit: number) {}

feed(data: unknown): string | Uint8Array | undefined {
const malformed = () => new WSChunkError(1007, "malformed chunked message");
if (typeof data === "string") { if (this.buf) throw malformed(); return data; }
const bytes = wsBytes(data);
const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
if (bytes.byteLength < 13 || view.getUint32(0) !== 0xffffffff) { if (this.buf) throw malformed(); return bytes; }
const kind = bytes[4] ?? 0;
const total = Number(view.getBigUint64(5));
const chunk = bytes.subarray(13);
if (kind > 1) throw malformed();
if (!this.buf) {
if (this.limit >= 0 && total > this.limit) throw new WSChunkError(1009, "chunked message exceeds the size limit");
this.buf = new Uint8Array(total);
this.filled = 0;
this.kind = kind;
} else if (total !== this.buf.byteLength || kind !== this.kind) {
throw malformed();
}
if (this.filled + chunk.byteLength > total) throw malformed();
this.buf.set(chunk, this.filled);
this.filled += chunk.byteLength;
if (this.filled < total) return undefined;
const out = this.buf;
this.buf = undefined;
return this.kind === 1 ? out : wsTextDecoder.decode(out);
}
}

export function wsSend(socket: { send(data: string | ArrayBuffer): void }, payload: string | ArrayBuffer): void {
let bytes: Uint8Array;
let kind = 1;
if (typeof payload === "string") {
if (payload.length <= WS_CHUNK_THRESHOLD / 3) { socket.send(payload); return; }
bytes = wsTextEncoder.encode(payload);
kind = 0;
} else {
bytes = new Uint8Array(payload);
}
if (bytes.byteLength <= WS_CHUNK_THRESHOLD) { socket.send(payload); return; }
for (let offset = 0; offset < bytes.byteLength; offset += WS_CHUNK_BYTES) {
const part = bytes.subarray(offset, Math.min(offset + WS_CHUNK_BYTES, bytes.byteLength));
const out = new Uint8Array(13 + part.byteLength);
const view = new DataView(out.buffer);
view.setUint32(0, 0xffffffff);
out[4] = kind;
view.setBigUint64(5, BigInt(bytes.byteLength));
out.set(part, 13);
socket.send(out.buffer);
}
}

export function wsEncodeMessage<T>(value: T, encode: (v: T) => unknown, split: ((v: T, raw: (Uint8Array | string)[]) => T) | undefined): string | ArrayBuffer {
if (split) {
const raw: (Uint8Array | string)[] = [];
const header = split(value, raw);
if (raw.some((segment) => (typeof segment === "string" ? segment.length : segment.byteLength) > 0)) return wsEncodeRawFrame(JSON.stringify(encode(header)), raw);
}
return JSON.stringify(encode(value));
}

export function wsDecodeMessage<T>(data: unknown, decode: (v: any) => T, join: ((v: T, raw: Uint8Array[], at: { i: number }) => boolean) | undefined): T {
if (typeof data === "string") return decode(JSON.parse(data));
const bytes = wsBytes(data);
if (!join) return decode(JSON.parse(wsTextDecoder.decode(bytes)));
const { header, raw } = wsDecodeRawFrame(bytes);
const value = decode(JSON.parse(header));
const at = { i: 0 };
if (!join(value, raw, at) || at.i !== raw.length) throw wsMalformedFrame();
return value;
}`
