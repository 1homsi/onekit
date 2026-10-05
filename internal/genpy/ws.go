package genpy

import (
	_ "embed"
	"fmt"

	"github.com/1homsi/onekit/internal/genshared"
	"github.com/1homsi/onekit/internal/onkir"
)

func writePyWSRuntime(p *Printer) {
	p.P(pyWSRuntimeSource)
}

func (p *Printer) isExternal(m *onkir.Message) bool {
	if p.resolver == nil {
		return false
	}
	_, ok := p.resolver.ResolveMessage(m)
	return ok
}

func fileMessagesDeep(file *onkir.File) []*onkir.Message {
	return genshared.FileMessagesDeep(file)
}

func wsFrameMessages(p *Printer, file *onkir.File) []*onkir.Message {
	seen := map[*onkir.Message]bool{}
	var out []*onkir.Message
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if !m.IsWebSocket() {
				continue
			}
			for _, frame := range []*onkir.Message{m.Request, m.Response} {
				if frame != nil && !seen[frame] && !p.isExternal(frame) {
					seen[frame] = true
					out = append(out, frame)
				}
			}
		}
	}
	return out
}

func pyWireInt(f *onkir.Field, expr string) string {
	if f.Type != nil && f.Type.Kind == onkir.KindScalar && (f.Type.Scalar == onkir.ScalarInt64 || f.Type.Scalar == onkir.ScalarUint64) {
		if genshared.NeedsInt64StringEncoding(f) {
			return "str(" + expr + ")"
		}
	}
	return expr
}

func pyIDValue(f *onkir.Field, expr string) string {
	if f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar != onkir.ScalarString {
		return "int(" + expr + ")"
	}
	return expr
}

func pyVariantTarget(f *onkir.Field, v *onkir.OneofVariant) string {
	if f.Oneof.Flatten() {
		return "o"
	}
	return fmt.Sprintf("o.get(%q)", v.Name)
}

func pyOneofMatch(f *onkir.Field, v *onkir.OneofVariant) string {
	return fmt.Sprintf("isinstance(o, dict) and o.get(%q) == %q", oneofDiscriminator(f), v.Tag())
}

func oneofDiscriminator(f *onkir.Field) string {
	return genshared.OneofDiscriminator(f)
}

func writePyWSCodecs(p *Printer, file *onkir.File) {
	for _, m := range fileMessagesDeep(file) {
		if onkir.MessageHasRaw(m, p.isExternal) {
			writePyRawFuncs(p, m)
		}
	}
	for _, m := range wsFrameMessages(p, file) {
		writePyFrameFuncs(p, m)
		writePyCodecClass(p, m)
	}
}

func writePyRawFuncs(p *Printer, m *onkir.Message) {
	steps := onkir.RawSteps(m, p.isExternal)
	p.P("def _ws_split_raw_", m.Name, "(d, raw):")
	p.Indent()
	p.P("d = dict(d)")
	for _, step := range steps {
		key := fmt.Sprintf("%q", step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("o = d.get(", key, ")")
			if step.Field.Oneof.Flatten() {
				p.P("if ", pyOneofMatch(step.Field, step.Variant), ":")
				p.Indent()
				p.P("d[", key, "] = _ws_split_raw_", step.Child.Name, "(o, raw)")
				p.Dedent()
				continue
			}
			vKey := fmt.Sprintf("%q", step.Variant.Name)
			p.P("if ", pyOneofMatch(step.Field, step.Variant), " and o.get(", vKey, ") is not None:")
			p.Indent()
			p.P("o = dict(o)")
			p.P("o[", vKey, "] = _ws_split_raw_", step.Child.Name, "(o[", vKey, "], raw)")
			p.P("d[", key, "] = o")
			p.Dedent()
		case step.Child != nil && step.Field.Repeated:
			p.P("if d.get(", key, ") is not None:")
			p.Indent()
			p.P("d[", key, "] = [_ws_split_raw_", step.Child.Name, "(item, raw) if item is not None else None for item in d[", key, "]]")
			p.Dedent()
		case step.Child != nil:
			p.P("if d.get(", key, ") is not None:")
			p.Indent()
			p.P("d[", key, "] = _ws_split_raw_", step.Child.Name, "(d[", key, "], raw)")
			p.Dedent()
		case step.Field.Type.Scalar == onkir.ScalarBytes:
			p.P("v = d.pop(", key, ", None)")
			p.P(`raw.append(_ws_base64.b64decode(v) if v else b"")`)
		default:
			p.P("raw.append((d.get(", key, `) or "").encode("utf-8"))`)
			p.P("d[", key, `] = ""`)
		}
	}
	p.P("return d")
	p.Dedent()
	p.Blank()
	p.P("def _ws_join_raw_", m.Name, "(d, it):")
	p.Indent()
	for _, step := range steps {
		key := fmt.Sprintf("%q", step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("o = d.get(", key, ")")
			target := pyVariantTarget(step.Field, step.Variant)
			p.P("if ", pyOneofMatch(step.Field, step.Variant), " and ", target, " is not None and not _ws_join_raw_", step.Child.Name, "(", target, ", it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
		case step.Child != nil && step.Field.Repeated:
			p.P("for item in d.get(", key, ") or []:")
			p.Indent()
			p.P("if item is not None and not _ws_join_raw_", step.Child.Name, "(item, it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
			p.Dedent()
		case step.Child != nil:
			p.P("if d.get(", key, ") is not None and not _ws_join_raw_", step.Child.Name, "(d[", key, "], it):")
			p.Indent()
			p.P("return False")
			p.Dedent()
		default:
			p.P("segment = next(it, None)")
			p.P("if segment is None:")
			p.Indent()
			p.P("return False")
			p.Dedent()
			if step.Field.Type.Scalar == onkir.ScalarBytes {
				p.P("d[", key, `] = _ws_base64.b64encode(segment).decode("ascii")`)
			} else {
				p.P("d[", key, `] = segment.decode("utf-8")`)
			}
		}
	}
	p.P("return True")
	p.Dedent()
	p.Blank()
}

func writePyFrameFuncs(p *Printer, m *onkir.Message) {
	idField, correlated := wsIDOf(m)
	if correlated {
		writePyReply(p, m)
		writePyVariant(p, m)
	}
	if _, _, cancelID, ok := onkir.WSCancelVariant(m); ok {
		f, v, _, _ := onkir.WSCancelVariant(m)
		p.P("def _ws_cancel_", m.Name, "(call_id):")
		p.Indent()
		value := pyWireInt(cancelID, "call_id")
		disc := oneofDiscriminator(f)
		if f.Oneof.Flatten() {
			p.P(fmt.Sprintf("return {%q: {%q: %q, %q: %s}}", f.Name, disc, v.Tag(), cancelID.Name, value))
		} else {
			p.P(fmt.Sprintf("return {%q: {%q: %q, %q: {%q: %s}}}", f.Name, disc, v.Tag(), v.Name, cancelID.Name, value))
		}
		p.Dedent()
		p.Blank()
	}
	if onkir.MessageHasWSTimeout(m) {
		writePyWithTimeout(p, m)
	}
	_ = idField
}

func wsIDOf(m *onkir.Message) (*onkir.Field, bool) {
	return onkir.WSIDField(m)
}

func writePyReply(p *Printer, m *onkir.Message) {
	p.P("def _ws_reply_", m.Name, "(d):")
	p.Indent()
	if f := onkir.FindWSIDDirect(m); f != nil {
		p.P("if ", fmt.Sprintf("%q", f.Name), " in d:")
		p.Indent()
		p.P("return (", pyIDValue(f, fmt.Sprintf("d[%q]", f.Name)), `, "")`)
		p.Dedent()
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
		for _, v := range f.Oneof.Variants {
			if v.IsWSCancel() || v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			vf := onkir.FindWSIDDirect(v.Type.Message)
			if vf == nil {
				continue
			}
			p.P("if ", pyOneofMatch(f, v), ":")
			p.Indent()
			p.P("v = ", pyVariantTarget(f, v))
			p.P("if isinstance(v, dict) and ", fmt.Sprintf("%q", vf.Name), " in v:")
			p.Indent()
			p.P("return (", pyIDValue(vf, fmt.Sprintf("v[%q]", vf.Name)), ", ", fmt.Sprintf("%q", v.Tag()), ")")
			p.Dedent()
			p.Dedent()
		}
	}
	p.P("return None")
	p.Dedent()
	p.Blank()
}

func writePyVariant(p *Printer, m *onkir.Message) {
	p.P("def _ws_variant_", m.Name, "(d):")
	p.Indent()
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage || onkir.FindWSIDDirect(v.Type.Message) == nil {
				continue
			}
			p.P("if ", pyOneofMatch(f, v), ":")
			p.Indent()
			p.P("return ", fmt.Sprintf("%q", v.Tag()))
			p.Dedent()
		}
	}
	p.P(`return ""`)
	p.Dedent()
	p.Blank()
}

func writePyWithTimeout(p *Printer, m *onkir.Message) {
	unset := "in (None, 0, \"0\", \"\")"
	p.P("def _ws_with_timeout_", m.Name, "(d, ms):")
	p.Indent()
	p.P("d = dict(d)")
	if f := onkir.WSTimeoutField(m); f != nil {
		p.P("if d.get(", fmt.Sprintf("%q", f.Name), ") ", unset, ":")
		p.Indent()
		p.P("d[", fmt.Sprintf("%q", f.Name), "] = ", pyWireInt(f, "ms"))
		p.Dedent()
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
			p.P("o = d.get(", fmt.Sprintf("%q", f.Name), ")")
			if f.Oneof.Flatten() {
				p.P("if ", pyOneofMatch(f, v), " and o.get(", fmt.Sprintf("%q", tf.Name), ") ", unset, ":")
				p.Indent()
				p.P("o = dict(o)")
				p.P("o[", fmt.Sprintf("%q", tf.Name), "] = ", pyWireInt(tf, "ms"))
				p.P("d[", fmt.Sprintf("%q", f.Name), "] = o")
				p.Dedent()
				continue
			}
			vKey := fmt.Sprintf("%q", v.Name)
			p.P("if ", pyOneofMatch(f, v), " and isinstance(o.get(", vKey, "), dict) and o[", vKey, "].get(", fmt.Sprintf("%q", tf.Name), ") ", unset, ":")
			p.Indent()
			p.P("o = dict(o)")
			p.P("inner = dict(o[", vKey, "])")
			p.P("inner[", fmt.Sprintf("%q", tf.Name), "] = ", pyWireInt(tf, "ms"))
			p.P("o[", vKey, "] = inner")
			p.P("d[", fmt.Sprintf("%q", f.Name), "] = o")
			p.Dedent()
		}
	}
	p.P("return d")
	p.Dedent()
	p.Blank()
}

func writePyCodecClass(p *Printer, m *onkir.Message) {
	p.P("class _", m.Name, "WsCodec(_WsCodec):")
	p.Indent()
	wrote := false
	if onkir.MessageHasRaw(m, p.isExternal) {
		p.P("split = staticmethod(_ws_split_raw_", m.Name, ")")
		p.P("join = staticmethod(_ws_join_raw_", m.Name, ")")
		wrote = true
	}
	if onkir.MessageHasWSTimeout(m) {
		p.P("with_timeout = staticmethod(_ws_with_timeout_", m.Name, ")")
		wrote = true
	}
	if _, _, _, ok := onkir.WSCancelVariant(m); ok {
		p.P("cancel = staticmethod(_ws_cancel_", m.Name, ")")
		wrote = true
	}
	if _, correlated := wsIDOf(m); correlated {
		p.P("reply = staticmethod(_ws_reply_", m.Name, ")")
		p.P("variant = staticmethod(_ws_variant_", m.Name, ")")
		wrote = true
	}
	if !wrote {
		p.P("pass")
	}
	p.Dedent()
	p.Blank()
}

func pyCodecName(p *Printer, m *onkir.Message) string {
	if p.isExternal(m) {
		return "_WsCodec"
	}
	return "_" + m.Name + "WsCodec"
}

func writePyWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	_, correlated := m.WSIDField()
	socketType := "WsFrameSocket"
	if correlated {
		socketType = "WsCallSocket"
	}
	p.P("def ", SnakeCase(m.Name), "(self, req: ", p.MessageTypeName(m.Request), ") -> ", socketType, ":")
	p.Indent()
	writePyDoc(p, m.Doc)
	writePyDeprecation(p, m)
	p.P(`if hasattr(req, "validate"): req.validate()`)
	p.P(fmt.Sprintf("path = %q", fullPath))
	writePyPathParams(p, wsPath, m.Request)
	writeClientQueryParams(p, m.Request)
	p.P("connection = _ws_connect(self.base_url + path, self.headers, self.max_ws_frame_bytes, self.ws_ping_interval)")
	p.P("return ", socketType, "(connection, ", p.MessageTypeName(m.Response), ", ", pyCodecName(p, m.Request), ", ", pyCodecName(p, m.Response), ", self.max_ws_message_bytes)")
	p.Dedent()
	p.Blank()
}

//go:embed runtime/ws.py
var pyWSRuntimeSource string
