package gengo

import (
	"github.com/1homsi/onekit/internal/onkir"
)

func (p *Printer) isExternal(m *onkir.Message) bool {
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

func fileHasWSRaw(p *Printer, file *onkir.File) bool {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsWebSocket() && (onkir.MessageHasRaw(m.Request, p.isExternal) || onkir.MessageHasRaw(m.Response, p.isExternal)) {
				return true
			}
		}
	}
	return false
}

func writeWSCodecRuntime(p *Printer, raw bool) {
	p.P("func wsMarshal(v any) ([]byte, error) {")
	p.P("if m, ok := v.(json.Marshaler); ok { return m.MarshalJSON() }")
	p.P("return json.Marshal(v)")
	p.P("}")
	p.P()
	p.P("func wsUnmarshal(data []byte, v any) error {")
	p.P("if u, ok := v.(json.Unmarshaler); ok { return u.UnmarshalJSON(data) }")
	p.P("return json.Unmarshal(data, v)")
	p.P("}")
	p.P()
	writeWSIORuntime(p)
	if !raw {
		p.P("func wsEncode(v any) (bool, []byte, [][]byte, error) {")
		p.P("data, err := wsMarshal(v)")
		p.P("return false, data, nil, err")
		p.P("}")
		p.P()
		p.P("func wsDecode(_ bool, data []byte, v any) error { return wsUnmarshal(data, v) }")
		p.P()
		return
	}
	p.P(`var errWSRawFrame = errors.New("malformed binary frame")`)
	p.P()
	p.P("func wsEncode(v any) (bool, []byte, [][]byte, error) {")
	p.P("if r, ok := v.(interface{ wsSplitRawAny() (any, [][]byte) }); ok {")
	p.P("header, raw := r.wsSplitRawAny()")
	p.P("size := 0")
	p.P("for _, segment := range raw { size += len(segment) }")
	p.P("if size > 0 {")
	p.P("data, err := wsMarshal(header)")
	p.P("if err != nil { return false, nil, nil, err }")
	p.P("return true, wsRawPrefix(data, raw), raw, nil")
	p.P("}")
	p.P("}")
	p.P("data, err := wsMarshal(v)")
	p.P("return false, data, nil, err")
	p.P("}")
	p.P()
	p.P("func wsRawPrefix(header []byte, raw [][]byte) []byte {")
	p.P("out := make([]byte, 0, 8+len(header)+4*len(raw))")
	p.P("out = binary.BigEndian.AppendUint32(out, uint32(len(header)))")
	p.P("out = append(out, header...)")
	p.P("out = binary.BigEndian.AppendUint32(out, uint32(len(raw)))")
	p.P("for _, segment := range raw { out = binary.BigEndian.AppendUint32(out, uint32(len(segment))) }")
	p.P("return out")
	p.P("}")
	p.P()
	p.P("func wsDecode(binaryFrame bool, data []byte, v any) error {")
	p.P("j, ok := v.(interface{ wsJoinRawAll([][]byte) bool })")
	p.P("if !binaryFrame || !ok { return wsUnmarshal(data, v) }")
	p.P("header, raw, err := wsDecodeRawFrame(data)")
	p.P("if err != nil { return err }")
	p.P("if err := wsUnmarshal(header, v); err != nil { return err }")
	p.P("if !j.wsJoinRawAll(raw) { return errWSRawFrame }")
	p.P("return nil")
	p.P("}")
	p.P()
	writeWSRawFrameRuntime(p)
	p.P("func wsRawBytes(s string) []byte {")
	p.P("if s == \"\" { return nil }")
	p.P("return unsafe.Slice(unsafe.StringData(s), len(s))")
	p.P("}")
	p.P()
	p.P("func wsRawString(b []byte) string {")
	p.P("if len(b) == 0 { return \"\" }")
	p.P("return unsafe.String(&b[0], len(b))")
	p.P("}")
	p.P()
}

func isRawString(f *onkir.Field) bool {
	return f.Type != nil && f.Type.Kind == onkir.KindScalar && f.Type.Scalar == onkir.ScalarString
}

func writeRawMethods(p *Printer, m *onkir.Message) {
	steps := onkir.RawSteps(m, p.isExternal)
	name := m.Name
	writeRawSplit(p, m, name, steps)
	writeRawJoin(p, m, name, steps)
	p.P("func (m *", name, ") wsSplitRawAny() (any, [][]byte) {")
	p.P("header, raw := m.wsSplitRaw(nil)")
	p.P("return header, raw")
	p.P("}")
	p.P()
	p.P("func (m *", name, ") wsJoinRawAll(raw [][]byte) bool {")
	p.P("rest, ok := m.wsJoinRaw(raw)")
	p.P("return ok && len(rest) == 0")
	p.P("}")
	p.P()
}

func writeRawSplit(p *Printer, m *onkir.Message, name string, steps []onkir.RawStep) {
	p.P("func (m *", name, ") wsSplitRaw(raw [][]byte) (*", name, ", [][]byte) {")
	p.P("if m == nil { return nil, raw }")
	p.P("c := *m")
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		field := "c." + PascalCase(step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("switch v := ", field, ".(type) {")
			for ; i < len(steps) && steps[i].Field == step.Field; i++ {
				variant := steps[i].Variant
				vName := PascalCase(variant.Name)
				p.P("case *", OneofVariantTypeName(m, step.Field, variant), ":")
				p.P("nv := *v")
				p.P("nv.", vName, ", raw = v.", vName, ".wsSplitRaw(raw)")
				p.P(field, " = &nv")
			}
			i--
			p.P("}")
		case step.Child != nil && step.Field.Repeated:
			p.P("if ", field, " != nil {")
			p.P("items := make([]*", p.MessageTypeName(step.Child), ", len(", field, "))")
			p.P("for i, item := range ", field, " { items[i], raw = item.wsSplitRaw(raw) }")
			p.P(field, " = items")
			p.P("}")
		case step.Child != nil:
			p.P(field, ", raw = ", field, ".wsSplitRaw(raw)")
		case isRawString(step.Field):
			p.P("raw = append(raw, wsRawBytes(", field, "))")
			p.P(field, ` = ""`)
		default:
			p.P("raw = append(raw, ", field, ")")
			p.P(field, " = nil")
		}
	}
	p.P("return &c, raw")
	p.P("}")
	p.P()
}

func writeRawJoin(p *Printer, m *onkir.Message, name string, steps []onkir.RawStep) {
	p.P("func (m *", name, ") wsJoinRaw(raw [][]byte) ([][]byte, bool) {")
	p.P("if m == nil { return raw, true }")
	for _, step := range steps {
		if step.Child != nil {
			p.P("var ok bool")
			break
		}
	}
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		field := "m." + PascalCase(step.Field.Name)
		switch {
		case step.Variant != nil:
			p.P("switch v := ", field, ".(type) {")
			for ; i < len(steps) && steps[i].Field == step.Field; i++ {
				variant := steps[i].Variant
				p.P("case *", OneofVariantTypeName(m, step.Field, variant), ":")
				p.P("if raw, ok = v.", PascalCase(variant.Name), ".wsJoinRaw(raw); !ok { return raw, false }")
			}
			i--
			p.P("}")
		case step.Child != nil && step.Field.Repeated:
			p.P("for _, item := range ", field, " { if raw, ok = item.wsJoinRaw(raw); !ok { return raw, false } }")
		case step.Child != nil:
			p.P("if raw, ok = ", field, ".wsJoinRaw(raw); !ok { return raw, false }")
		default:
			p.P("if len(raw) == 0 { return raw, false }")
			if isRawString(step.Field) {
				p.P(field, " = wsRawString(raw[0])")
			} else {
				p.P(field, " = raw[0]")
			}
			p.P("raw = raw[1:]")
		}
	}
	p.P("return raw, true")
	p.P("}")
	p.P()
}

func writeWSIORuntime(p *Printer) {
	p.P("func wsReadAll(r io.Reader, buf []byte) ([]byte, error) {")
	p.P("for {")
	p.P("if len(buf) == cap(buf) {")
	p.P("var probe [1]byte")
	p.P("n, err := r.Read(probe[:])")
	p.P("if n == 0 && err == io.EOF { return buf, nil }")
	p.P("if err != nil && err != io.EOF { return nil, err }")
	p.P("buf = append(buf, probe[:n]...)")
	p.P("if err == io.EOF { return buf, nil }")
	p.P("continue")
	p.P("}")
	p.P("n, err := r.Read(buf[len(buf):cap(buf)])")
	p.P("buf = buf[:len(buf)+n]")
	p.P("if err == io.EOF { return buf, nil }")
	p.P("if err != nil { return nil, err }")
	p.P("}")
	p.P("}")
	p.P()
	p.P("func wsWriteParts(w io.WriteCloser, prefix []byte, raw [][]byte) error {")
	p.P("pending := prefix")
	p.P("for _, segment := range raw {")
	p.P("if len(segment) < 32<<10 {")
	p.P("pending = append(pending, segment...)")
	p.P("continue")
	p.P("}")
	p.P("if len(pending) > 0 {")
	p.P("if _, err := w.Write(pending); err != nil { _ = w.Close(); return err }")
	p.P("pending = pending[:0]")
	p.P("}")
	p.P("if _, err := w.Write(segment); err != nil { _ = w.Close(); return err }")
	p.P("}")
	p.P("if len(pending) > 0 {")
	p.P("if _, err := w.Write(pending); err != nil { _ = w.Close(); return err }")
	p.P("}")
	p.P("return w.Close()")
	p.P("}")
	p.P()
}

func writeWSRawFrameRuntime(p *Printer) {
	p.P("func wsEncodeRawFrame(header []byte, raw [][]byte, size int) []byte {")
	p.P("out := make([]byte, 0, 8+len(header)+4*len(raw)+size)")
	p.P("out = binary.BigEndian.AppendUint32(out, uint32(len(header)))")
	p.P("out = append(out, header...)")
	p.P("out = binary.BigEndian.AppendUint32(out, uint32(len(raw)))")
	p.P("for _, segment := range raw { out = binary.BigEndian.AppendUint32(out, uint32(len(segment))) }")
	p.P("for _, segment := range raw { out = append(out, segment...) }")
	p.P("return out")
	p.P("}")
	p.P()
	p.P("func wsDecodeRawFrame(data []byte) ([]byte, [][]byte, error) {")
	p.P("if len(data) < 4 { return nil, nil, errWSRawFrame }")
	p.P("headerLen := uint64(binary.BigEndian.Uint32(data))")
	p.P("data = data[4:]")
	p.P("if headerLen+4 > uint64(len(data)) { return nil, nil, errWSRawFrame }")
	p.P("header := data[:headerLen:headerLen]")
	p.P("data = data[headerLen:]")
	p.P("count := uint64(binary.BigEndian.Uint32(data))")
	p.P("data = data[4:]")
	p.P("if count*4 > uint64(len(data)) { return nil, nil, errWSRawFrame }")
	p.P("lengths := data[:count*4]")
	p.P("data = data[count*4:]")
	p.P("raw := make([][]byte, count)")
	p.P("for i := range raw {")
	p.P("n := uint64(binary.BigEndian.Uint32(lengths[4*i:]))")
	p.P("if n > uint64(len(data)) { return nil, nil, errWSRawFrame }")
	p.P("raw[i] = data[:n:n]")
	p.P("data = data[n:]")
	p.P("}")
	p.P("if len(data) != 0 { return nil, nil, errWSRawFrame }")
	p.P("return header, raw, nil")
	p.P("}")
	p.P()
}
