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
			for _, frame := range frames(m) {
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
	p.P("const wsTextEncoder = new TextEncoder();")
	p.P("const wsTextDecoder = new TextDecoder();")
	p.P()
	p.P("function wsMalformedFrame(): Error {")
	p.P(`return new Error("malformed binary frame");`)
	p.P("}")
	p.P()
	p.P("function wsBytes(data: unknown): Uint8Array {")
	p.P("if (data instanceof Uint8Array) return data;")
	p.P("if (data instanceof ArrayBuffer) return new Uint8Array(data);")
	p.P("if (ArrayBuffer.isView(data)) return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);")
	p.P("throw wsMalformedFrame();")
	p.P("}")
	p.P()
	p.P("export function wsEncodeRawFrame(header: string, raw: (Uint8Array | string)[]): ArrayBuffer {")
	p.P("const head = wsTextEncoder.encode(header);")
	p.P("let size = 0;")
	p.P(`for (const segment of raw) size += typeof segment === "string" ? segment.length : segment.byteLength;`)
	p.P("const out = new Uint8Array(8 + head.byteLength + 4 * raw.length + size);")
	p.P("const view = new DataView(out.buffer);")
	p.P("view.setUint32(0, head.byteLength);")
	p.P("out.set(head, 4);")
	p.P("let offset = 4 + head.byteLength;")
	p.P("view.setUint32(offset, raw.length);")
	p.P("offset += 4;")
	p.P("let data = offset + 4 * raw.length;")
	p.P("for (const segment of raw) {")
	p.P(`if (typeof segment === "string") {`)
	p.P("const written = wsTextEncoder.encodeInto(segment, out.subarray(data, data + segment.length));")
	p.P("if (written.read !== segment.length) return wsEncodeRawFrame(header, raw.map((s) => typeof s === \"string\" ? wsTextEncoder.encode(s) : s));")
	p.P("view.setUint32(offset, written.written);")
	p.P("data += written.written;")
	p.P("} else {")
	p.P("view.setUint32(offset, segment.byteLength);")
	p.P("out.set(segment, data);")
	p.P("data += segment.byteLength;")
	p.P("}")
	p.P("offset += 4;")
	p.P("}")
	p.P("return out.buffer;")
	p.P("}")
	p.P()
	p.P("export function wsDecodeRawFrame(data: Uint8Array): { header: string; raw: Uint8Array[] } {")
	p.P("const view = new DataView(data.buffer, data.byteOffset, data.byteLength);")
	p.P("if (data.byteLength < 4) throw wsMalformedFrame();")
	p.P("const headerLen = view.getUint32(0);")
	p.P("let offset = 4;")
	p.P("if (offset + headerLen + 4 > data.byteLength) throw wsMalformedFrame();")
	p.P("const header = wsTextDecoder.decode(data.subarray(offset, offset + headerLen));")
	p.P("offset += headerLen;")
	p.P("const count = view.getUint32(offset);")
	p.P("offset += 4;")
	p.P("if (offset + count * 4 > data.byteLength) throw wsMalformedFrame();")
	p.P("const lengths: number[] = [];")
	p.P("for (let i = 0; i < count; i++) { lengths.push(view.getUint32(offset)); offset += 4; }")
	p.P("const raw: Uint8Array[] = [];")
	p.P("for (const n of lengths) {")
	p.P("if (offset + n > data.byteLength) throw wsMalformedFrame();")
	p.P("raw.push(data.subarray(offset, offset + n));")
	p.P("offset += n;")
	p.P("}")
	p.P("if (offset !== data.byteLength) throw wsMalformedFrame();")
	p.P("return { header, raw };")
	p.P("}")
	p.P()
	p.P("export function wsEncodeMessage<T>(value: T, encode: (v: T) => unknown, split: ((v: T, raw: (Uint8Array | string)[]) => T) | undefined): string | ArrayBuffer {")
	p.P("if (split) {")
	p.P("const raw: (Uint8Array | string)[] = [];")
	p.P("const header = split(value, raw);")
	p.P(`if (raw.some((segment) => (typeof segment === "string" ? segment.length : segment.byteLength) > 0)) return wsEncodeRawFrame(JSON.stringify(encode(header)), raw);`)
	p.P("}")
	p.P("return JSON.stringify(encode(value));")
	p.P("}")
	p.P()
	p.P("export function wsDecodeMessage<T>(data: unknown, decode: (v: any) => T, join: ((v: T, raw: Uint8Array[], at: { i: number }) => boolean) | undefined): T {")
	p.P(`if (typeof data === "string") return decode(JSON.parse(data));`)
	p.P("const bytes = wsBytes(data);")
	p.P("if (!join) return decode(JSON.parse(wsTextDecoder.decode(bytes)));")
	p.P("const { header, raw } = wsDecodeRawFrame(bytes);")
	p.P("const value = decode(JSON.parse(header));")
	p.P("const at = { i: 0 };")
	p.P("if (!join(value, raw, at) || at.i !== raw.length) throw wsMalformedFrame();")
	p.P("return value;")
	p.P("}")
	p.P()
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
			p.P(fmt.Sprintf("if (%s && %s.%s === %q) %s = { ...%s, %s: %s(%s.%s, raw) };", prop, prop, disc, step.Variant.Tag(), prop, prop, vProp, split, prop, vProp))
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
			p.P(fmt.Sprintf("if (%s && %s.%s === %q && !%s(%s, raw, at)) return false;", prop, prop, disc, step.Variant.Tag(), join, target))
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
