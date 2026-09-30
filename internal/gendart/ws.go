package gendart

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

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

func writeWSCodecs(p *Printer, file *onkir.File) {
	for _, m := range fileMessagesDeep(file) {
		if onkir.MessageHasRaw(m, p.isExternal) {
			writeRawFuncs(p, m)
		}
	}
	for _, m := range wsFrameMessages(p, file) {
		writeFrameFuncs(p, m)
		writeCodecConst(p, m)
	}
}

func oneofMatch(f *onkir.Field, v *onkir.OneofVariant, expr string) string {
	return fmt.Sprintf("%s is Map && %s[%s] == %s", expr, expr, dartString(oneofDiscriminator(f)), dartString(v.Tag()))
}

func variantTarget(f *onkir.Field, v *onkir.OneofVariant, expr string) string {
	if f.Oneof.Flatten() {
		return expr
	}
	return expr + "[" + dartString(v.Name) + "]"
}

func wireInt(f *onkir.Field, expr string) string {
	if int64AsString(f) {
		return expr + ".toString()"
	}
	return expr
}

func wireID(f *onkir.Field, expr string) string {
	if f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar != onkir.ScalarString {
		return "onekitInt(" + expr + ")"
	}
	return expr + " as String"
}

func rawName(m *onkir.Message) string { return MessageName(m) }

func writeRawFuncs(p *Printer, m *onkir.Message) {
	steps := onkir.RawSteps(m, p.isExternal)
	name := rawName(m)
	p.P("Map<String, dynamic> _wsSplitRaw", name, "(Map<String, dynamic> json, List<Uint8List> raw) {")
	p.Indent()
	p.P("final d = Map<String, dynamic>.of(json);")
	for _, step := range steps {
		key := dartString(step.Field.Name)
		switch {
		case step.Variant != nil:
			child := "_wsSplitRaw" + rawName(step.Child)
			p.P("{")
			p.Indent()
			p.P("final o = d[", key, "];")
			if step.Field.Oneof.Flatten() {
				p.P("if (", oneofMatch(step.Field, step.Variant, "o"), ") d[", key, "] = ", child, "(o as Map<String, dynamic>, raw);")
			} else {
				vKey := dartString(step.Variant.Name)
				p.P("if (", oneofMatch(step.Field, step.Variant, "o"), " && o[", vKey, "] != null) {")
				p.Indent()
				p.P("final copy = Map<String, dynamic>.of(o as Map<String, dynamic>);")
				p.P("copy[", vKey, "] = ", child, "(copy[", vKey, "] as Map<String, dynamic>, raw);")
				p.P("d[", key, "] = copy;")
				p.Dedent()
				p.P("}")
			}
			p.Dedent()
			p.P("}")
		case step.Child != nil && step.Field.Repeated:
			p.P("if (d[", key, "] != null) {")
			p.Indent()
			p.P("d[", key, "] = [for (final item in d[", key, "] as List) item == null ? null : _wsSplitRaw", rawName(step.Child), "(item as Map<String, dynamic>, raw)];")
			p.Dedent()
			p.P("}")
		case step.Child != nil:
			p.P("if (d[", key, "] != null) d[", key, "] = _wsSplitRaw", rawName(step.Child), "(d[", key, "] as Map<String, dynamic>, raw);")
		case step.Field.Type.Scalar == onkir.ScalarBytes:
			p.P("{")
			p.Indent()
			p.P("final v = d.remove(", key, ");")
			p.P("raw.add(v == null || v == '' ? Uint8List(0) : base64.decode(v as String));")
			p.Dedent()
			p.P("}")
		default:
			p.P("raw.add(utf8.encode((d[", key, "] as String?) ?? ''));")
			p.P("d[", key, "] = '';")
		}
	}
	p.P("return d;")
	p.Dedent()
	p.P("}")
	p.P()
	p.P("bool _wsJoinRaw", name, "(Map<String, dynamic> d, Iterator<Uint8List> it) {")
	p.Indent()
	for _, step := range steps {
		key := dartString(step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("{")
			p.Indent()
			p.P("final o = d[", key, "];")
			target := variantTarget(step.Field, step.Variant, "o")
			p.P("if (", oneofMatch(step.Field, step.Variant, "o"), " && ", target, " != null && !_wsJoinRaw", rawName(step.Child), "(", target, " as Map<String, dynamic>, it)) return false;")
			p.Dedent()
			p.P("}")
		case step.Child != nil && step.Field.Repeated:
			p.P("for (final item in (d[", key, "] as List?) ?? const []) {")
			p.Indent()
			p.P("if (item != null && !_wsJoinRaw", rawName(step.Child), "(item as Map<String, dynamic>, it)) return false;")
			p.Dedent()
			p.P("}")
		case step.Child != nil:
			p.P("if (d[", key, "] != null && !_wsJoinRaw", rawName(step.Child), "(d[", key, "] as Map<String, dynamic>, it)) return false;")
		default:
			p.P("if (!it.moveNext()) return false;")
			if step.Field.Type.Scalar == onkir.ScalarBytes {
				p.P("d[", key, "] = base64.encode(it.current);")
			} else {
				p.P("d[", key, "] = utf8.decode(it.current);")
			}
		}
	}
	p.P("return true;")
	p.Dedent()
	p.P("}")
	p.P()
}

func writeFrameFuncs(p *Printer, m *onkir.Message) {
	name := MessageName(m)
	if _, correlated := onkir.WSIDField(m); correlated {
		writeReply(p, m)
		writeVariant(p, m)
	}
	if f, v, cancelID, ok := onkir.WSCancelVariant(m); ok {
		disc := dartString(oneofDiscriminator(f))
		value := wireInt(cancelID, "callId")
		if cancelID.Type.Scalar == onkir.ScalarString {
			value = "callId"
		}
		p.P("Map<String, dynamic> _wsCancel", name, "(Object callId) => {")
		p.Indent()
		if f.Oneof.Flatten() {
			p.P(dartString(f.Name), ": {", disc, ": ", dartString(v.Tag()), ", ", dartString(cancelID.Name), ": ", value, "},")
		} else {
			p.P(dartString(f.Name), ": {", disc, ": ", dartString(v.Tag()), ", ", dartString(v.Name), ": {", dartString(cancelID.Name), ": ", value, "}},")
		}
		p.Dedent()
		p.P("};")
		p.P()
	}
	if onkir.MessageHasWSTimeout(m) {
		writeWithTimeout(p, m)
	}
}

func writeReply(p *Printer, m *onkir.Message) {
	p.P("(Object, String)? _wsReply", MessageName(m), "(Map<String, dynamic> d) {")
	p.Indent()
	if f := onkir.FindWSIDDirect(m); f != nil {
		p.P("if (d.containsKey(", dartString(f.Name), ")) return (", wireID(f, "d["+dartString(f.Name)+"]"), ", '');")
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		p.P("{")
		p.Indent()
		p.P("final o = d[", dartString(f.Name), "];")
		for _, v := range f.Oneof.Variants {
			if v.IsWSCancel() || v.Type == nil || v.Type.Kind != onkir.KindMessage {
				continue
			}
			vf := onkir.FindWSIDDirect(v.Type.Message)
			if vf == nil {
				continue
			}
			p.P("if (", oneofMatch(f, v, "o"), ") {")
			p.Indent()
			p.P("final v = ", variantTarget(f, v, "o"), ";")
			p.P("if (v is Map && v.containsKey(", dartString(vf.Name), ")) return (", wireID(vf, "v["+dartString(vf.Name)+"]"), ", ", dartString(v.Tag()), ");")
			p.Dedent()
			p.P("}")
		}
		p.Dedent()
		p.P("}")
	}
	p.P("return null;")
	p.Dedent()
	p.P("}")
	p.P()
}

func writeVariant(p *Printer, m *onkir.Message) {
	p.P("String _wsVariant", MessageName(m), "(Map<String, dynamic> d) {")
	p.Indent()
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, v := range f.Oneof.Variants {
			if v.Type == nil || v.Type.Kind != onkir.KindMessage || onkir.FindWSIDDirect(v.Type.Message) == nil {
				continue
			}
			o := "d[" + dartString(f.Name) + "]"
			p.P("if (", oneofMatch(f, v, o), ") return ", dartString(v.Tag()), ";")
		}
	}
	p.P("return '';")
	p.Dedent()
	p.P("}")
	p.P()
}

func writeWithTimeout(p *Printer, m *onkir.Message) {
	unset := func(expr string) string {
		return "(" + expr + " == null || " + expr + " == 0 || " + expr + " == '0' || " + expr + " == '')"
	}
	p.P("Map<String, dynamic> _wsWithTimeout", MessageName(m), "(Map<String, dynamic> json, int ms) {")
	p.Indent()
	p.P("final d = Map<String, dynamic>.of(json);")
	if f := onkir.WSTimeoutField(m); f != nil {
		key := dartString(f.Name)
		p.P("if ", unset("d["+key+"]"), " d[", key, "] = ", wireInt(f, "ms"), ";")
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
			fKey, tKey := dartString(f.Name), dartString(tf.Name)
			p.P("{")
			p.Indent()
			p.P("final o = d[", fKey, "];")
			if f.Oneof.Flatten() {
				p.P("if (", oneofMatch(f, v, "o"), " && ", unset("o["+tKey+"]"), ") {")
				p.Indent()
				p.P("d[", fKey, "] = {...o as Map<String, dynamic>, ", tKey, ": ", wireInt(tf, "ms"), "};")
				p.Dedent()
				p.P("}")
			} else {
				vKey := dartString(v.Name)
				p.P("if (", oneofMatch(f, v, "o"), " && o[", vKey, "] is Map && ", unset("o["+vKey+"]["+tKey+"]"), ") {")
				p.Indent()
				p.P("d[", fKey, "] = {...o as Map<String, dynamic>, ", vKey, ": {...o[", vKey, "] as Map<String, dynamic>, ", tKey, ": ", wireInt(tf, "ms"), "}};")
				p.Dedent()
				p.P("}")
			}
			p.Dedent()
			p.P("}")
		}
	}
	p.P("return d;")
	p.Dedent()
	p.P("}")
	p.P()
}

func codecName(p *Printer, m *onkir.Message) string {
	if p.isExternal(m) {
		return "const WsCodec()"
	}
	return "_ws" + MessageName(m) + "Codec"
}

func writeCodecConst(p *Printer, m *onkir.Message) {
	name := MessageName(m)
	var args []string
	if onkir.MessageHasRaw(m, p.isExternal) {
		args = append(args, "split: _wsSplitRaw"+name, "join: _wsJoinRaw"+name)
	}
	if onkir.MessageHasWSTimeout(m) {
		args = append(args, "withTimeout: _wsWithTimeout"+name)
	}
	if _, _, _, ok := onkir.WSCancelVariant(m); ok {
		args = append(args, "cancel: _wsCancel"+name)
	}
	if _, correlated := onkir.WSIDField(m); correlated {
		args = append(args, "reply: _wsReply"+name, "variant: _wsVariant"+name)
	}
	p.P("const WsCodec _ws", name, "Codec = WsCodec(")
	p.Indent()
	for _, arg := range args {
		p.P(arg, ",")
	}
	p.Dedent()
	p.P(");")
	p.P()
}

func writeWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	route, _ := m.WebSocketPath()
	_, correlated := m.WSIDField()
	socket := "WsFrameSocket"
	if correlated {
		socket = "WsCallSocket"
	}
	req, res := p.MessageTypeName(m.Request), p.MessageTypeName(m.Response)
	writeMethodHeader(p, m)
	p.P("Future<", socket, "<", req, ", ", res, ">> ", MethodIdent(m.Name), "(", req, " req, {Map<String, String>? headers}) async {")
	p.Indent()
	p.P("onekitCheck(req.validate());")
	writePathAndQuery(p, s.BasePath+route, m.Request, true)
	p.P("final channel = await onekitWsConnect(onekitWsUri(baseUrl, path, query), onekitHeaders(this.headers, headers), wsPingInterval);")
	p.P("return ", socket, "<", req, ", ", res, ">(")
	p.Indent()
	p.P("channel,")
	p.P("encode: (frame) => frame.toJson(),")
	p.P("validate: (frame) => frame.validate(),")
	p.P("decode: ", res, ".fromJson,")
	p.P("sendCodec: ", codecName(p, m.Request), ",")
	p.P("recvCodec: ", codecName(p, m.Response), ",")
	p.P("maxFrameBytes: maxWsFrameBytes,")
	p.P("maxMessageBytes: maxWsMessageBytes,")
	p.Dedent()
	p.P(");")
	p.Dedent()
	p.P("}")
	p.P()
}
