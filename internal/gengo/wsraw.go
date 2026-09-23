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
	p.P("func wsMarshalAppend(dst []byte, v any) ([]byte, error) {")
	p.P("if f, ok := v.(interface{ wsAppendJSON([]byte) ([]byte, error) }); ok {")
	p.P("if b, err := f.wsAppendJSON(dst); err == nil { return b, nil }")
	p.P("}")
	p.P("var data []byte")
	p.P("var err error")
	p.P("if m, ok := v.(json.Marshaler); ok { data, err = m.MarshalJSON() } else { data, err = json.Marshal(v) }")
	p.P("if err != nil { return dst, err }")
	p.P("return append(dst, data...), nil")
	p.P("}")
	p.P()
	p.P("func wsMarshal(v any) ([]byte, error) { return wsMarshalAppend(nil, v) }")
	p.P()
	p.P("var wsBuffers = sync.Pool{New: func() any { b := make([]byte, 0, 1024); return &b }}")
	p.P()
	p.P("func wsGetBuffer() *[]byte { return wsBuffers.Get().(*[]byte) }")
	p.P()
	p.P("func wsPutBuffer(b *[]byte) {")
	p.P("if cap(*b) > 0 && cap(*b) <= 64<<10 {")
	p.P("*b = (*b)[:0]")
	p.P("wsBuffers.Put(b)")
	p.P("}")
	p.P("}")
	p.P()
	p.P("func wsEncode(v any) (bool, []byte, [][]byte, error) { return wsEncodeAppend(nil, v) }")
	p.P()
	p.P("func wsUnmarshal(data []byte, v any) error {")
	p.P("if f, ok := v.(interface { wsDecodeJSON(*wsJSON); wsResetJSON() }); ok {")
	p.P("d := wsJSON{data: data}")
	p.P("f.wsDecodeJSON(&d)")
	p.P("if d.end() { return nil }")
	p.P("f.wsResetJSON()")
	p.P("}")
	p.P("if u, ok := v.(json.Unmarshaler); ok { return u.UnmarshalJSON(data) }")
	p.P("return json.Unmarshal(data, v)")
	p.P("}")
	p.P()
	writeWSIORuntime(p)
	if !raw {
		p.P("func wsEncodeAppend(dst []byte, v any) (bool, []byte, [][]byte, error) {")
		p.P("data, err := wsMarshalAppend(dst, v)")
		p.P("return false, data, nil, err")
		p.P("}")
		p.P()
		p.P("func wsDecode(_ bool, data []byte, v any) error { return wsUnmarshal(data, v) }")
		p.P()
		return
	}
	p.P(`var errWSRawFrame = errors.New("malformed binary frame")`)
	p.P()
	p.P("func wsEncodeAppend(dst []byte, v any) (bool, []byte, [][]byte, error) {")
	p.P("if r, ok := v.(interface{ wsSplitRawAny() (any, [][]byte) }); ok {")
	p.P("header, raw := r.wsSplitRawAny()")
	p.P("size := 0")
	p.P("for _, segment := range raw { size += len(segment) }")
	p.P("if size > 0 {")
	p.P("out, err := wsMarshalAppend(append(dst, 0, 0, 0, 0), header)")
	p.P("if err != nil { return false, nil, nil, err }")
	p.P("binary.BigEndian.PutUint32(out[len(dst):], uint32(len(out)-len(dst)-4))")
	p.P("out = binary.BigEndian.AppendUint32(out, uint32(len(raw)))")
	p.P("for _, segment := range raw { out = binary.BigEndian.AppendUint32(out, uint32(len(segment))) }")
	p.P("return true, out, raw, nil")
	p.P("}")
	p.P("}")
	p.P("data, err := wsMarshalAppend(dst, v)")
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
	p.P(wsIORuntimeSource)
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

func writeWSTimeoutMethod(p *Printer, m *onkir.Message) {
	name := m.Name
	p.P("func (m *", name, ") wsWithTimeout(ms int64) *", name, " {")
	p.P("if m == nil || ms <= 0 { return m }")
	p.P("c := *m")
	if f := onkir.WSTimeoutField(m); f != nil {
		field := "c." + PascalCase(f.Name)
		p.P("if ", field, " == 0 { ", field, " = ", p.GoFieldType(f.Type), "(ms) }")
	}
	for _, f := range m.Fields {
		if f.Oneof == nil {
			continue
		}
		var cases []*onkir.OneofVariant
		for _, v := range f.Oneof.Variants {
			if v.Type != nil && v.Type.Kind == onkir.KindMessage && onkir.WSTimeoutField(v.Type.Message) != nil {
				cases = append(cases, v)
			}
		}
		if len(cases) == 0 {
			continue
		}
		p.P("switch v := c.", PascalCase(f.Name), ".(type) {")
		for _, v := range cases {
			vName := PascalCase(v.Name)
			tf := onkir.WSTimeoutField(v.Type.Message)
			p.P("case *", OneofVariantTypeName(m, f, v), ":")
			p.P("if v.", vName, " != nil && v.", vName, ".", PascalCase(tf.Name), " == 0 {")
			p.P("inner := *v.", vName)
			p.P("inner.", PascalCase(tf.Name), " = ", p.GoFieldType(tf.Type), "(ms)")
			p.P("c.", PascalCase(f.Name), " = &", OneofVariantTypeName(m, f, v), "{", vName, ": &inner}")
			p.P("}")
		}
		p.P("}")
	}
	p.P("return &c")
	p.P("}")
	p.P()
}

func writeWSDeadlineStamp(p *Printer, sent *onkir.Message) {
	if !onkir.MessageHasWSTimeout(sent) {
		return
	}
	p.P("if deadline, ok := ctx.Deadline(); ok { value = value.wsWithTimeout(max(time.Until(deadline).Milliseconds(), 1)) }")
}

const wsIORuntimeSource = `const (
wsChunkMarker = 0xFFFFFFFF
wsChunkHeader = 13
wsChunkBytes = 8 << 20
wsChunkThreshold = 16 << 20
)

var errWSChunk = errors.New("malformed chunked message")

var errWSMessageTooBig = errors.New("chunked message exceeds the size limit")

func wsMessageLimit(limit int64) int64 {
if limit == 0 { return 256 << 20 }
if limit < 0 { return math.MaxInt64 }
return limit
}

func wsChunkCloseCode(err error) int {
if errors.Is(err, errWSMessageTooBig) { return 1009 }
return 1007
}

type wsAssembler struct {
limit int64
buf []byte
total uint64
kind byte
active bool
}

func (a *wsAssembler) feed(isBinary bool, data []byte) (bool, []byte, bool, bool, error) {
if !isBinary || len(data) < wsChunkHeader || binary.BigEndian.Uint32(data) != wsChunkMarker {
if a.active { return false, nil, false, false, errWSChunk }
return isBinary, data, true, true, nil
}
kind := data[4]
total := binary.BigEndian.Uint64(data[5:wsChunkHeader])
chunk := data[wsChunkHeader:]
if kind > 1 { return false, nil, false, false, errWSChunk }
if !a.active {
if total > uint64(wsMessageLimit(a.limit)) { return false, nil, false, false, errWSMessageTooBig }
a.buf, a.total, a.kind, a.active = make([]byte, 0, total), total, kind, true
} else if total != a.total || kind != a.kind {
return false, nil, false, false, errWSChunk
}
if uint64(len(a.buf))+uint64(len(chunk)) > a.total { return false, nil, false, false, errWSChunk }
a.buf = append(a.buf, chunk...)
if uint64(len(a.buf)) < a.total { return false, nil, false, false, nil }
out := a.buf
a.buf, a.active = nil, false
return a.kind == 1, out, true, false, nil
}

func wsFrameSize(data []byte, raw [][]byte) int {
size := len(data)
for _, segment := range raw { size += len(segment) }
return size
}

func wsWriteChunked(newWriter func() (io.WriteCloser, error), isBinary bool, parts [][]byte) error {
total := 0
for _, part := range parts { total += len(part) }
var header [wsChunkHeader]byte
binary.BigEndian.PutUint32(header[:4], wsChunkMarker)
if isBinary { header[4] = 1 }
binary.BigEndian.PutUint64(header[5:], uint64(total))
index, offset, sent := 0, 0, 0
for {
w, err := newWriter()
if err != nil { return err }
if _, err := w.Write(header[:]); err != nil { _ = w.Close(); return err }
room := wsChunkBytes
for room > 0 && index < len(parts) {
part := parts[index][offset:]
n := min(room, len(part))
if n > 0 {
if _, err := w.Write(part[:n]); err != nil { _ = w.Close(); return err }
}
room -= n
sent += n
offset += n
if offset == len(parts[index]) { index, offset = index+1, 0 }
}
if err := w.Close(); err != nil { return err }
if sent >= total { return nil }
}
}

func wsPingInterval(interval time.Duration) time.Duration {
if interval == 0 { return 30 * time.Second }
return interval
}

func wsKeepAlive(ctx context.Context, ping func(context.Context) error, closeNow func() error, interval time.Duration) {
if interval <= 0 { return }
ticker := time.NewTicker(interval)
defer ticker.Stop()
for {
select {
case <-ctx.Done():
return
case <-ticker.C:
pingCtx, cancel := context.WithTimeout(ctx, interval)
err := ping(pingCtx)
cancel()
if err != nil {
if ctx.Err() == nil { _ = closeNow() }
return
}
}
}
}

func wsReadAll(r io.Reader, buf []byte) ([]byte, error) {
for {
if len(buf) == cap(buf) {
var probe [1]byte
n, err := r.Read(probe[:])
if n == 0 && err == io.EOF { return buf, nil }
if err != nil && err != io.EOF { return nil, err }
buf = append(buf, probe[:n]...)
if err == io.EOF { return buf, nil }
continue
}
n, err := r.Read(buf[len(buf):cap(buf)])
buf = buf[:len(buf)+n]
if err == io.EOF { return buf, nil }
if err != nil { return nil, err }
}
}

func wsWriteParts(w io.WriteCloser, prefix []byte, raw [][]byte) error {
pending := prefix
for _, segment := range raw {
if len(segment) < 32<<10 {
pending = append(pending, segment...)
continue
}
if len(pending) > 0 {
if _, err := w.Write(pending); err != nil { _ = w.Close(); return err }
pending = pending[:0]
}
if _, err := w.Write(segment); err != nil { _ = w.Close(); return err }
}
if len(pending) > 0 {
if _, err := w.Write(pending); err != nil { _ = w.Close(); return err }
}
return w.Close()
}`
