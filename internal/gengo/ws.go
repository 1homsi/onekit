package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

// --- shared runtime -------------------------------------------------------

// writeWSOutType emits the server-side send interface for bidirectional
// WebSocket methods. Handlers push response frames through it while the
// generated read loop owns the connection. Writes are mutex-guarded because
// a handler may retain out and call Send from a goroutine it spawned,
// concurrently with the read loop's own protocol-error replies.
func writeWSOutType(p *Printer) {
	p.P("// WSOut sends one direction of a bidirectional WebSocket RPC: server-")
	p.P("// to-client frames of the method's declared response type.")
	p.P("type WSOut[E any] interface {")
	p.P("Send(ctx context.Context, value *E) error")
	p.P("Context() context.Context")
	p.P("Close(code websocket.StatusCode, reason string) error")
	p.P("}")
	p.P()
	p.P("type wsConnOut[E any] struct {")
	p.P("conn *websocket.Conn")
	p.P("ctx context.Context")
	p.P("mu sync.Mutex")
	p.P("}")
	p.P()
	p.P("func (s *wsConnOut[E]) Context() context.Context { return s.ctx }")
	p.P()
	p.P("func (s *wsConnOut[E]) Close(code websocket.StatusCode, reason string) error { return s.conn.Close(code, wsCloseReason(reason)) }")
	p.P()
	p.P("func (s *wsConnOut[E]) Send(ctx context.Context, value *E) error {")
	p.P("buf := wsGetBuffer()")
	p.P("defer wsPutBuffer(buf)")
	p.P("binaryFrame, data, raw, err := wsEncodeAppend((*buf)[:0], value)")
	p.P("if cap(data) > 0 { *buf = data[:0] }")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("typ := websocket.MessageText")
	p.P("if binaryFrame { typ = websocket.MessageBinary }")
	p.P("s.mu.Lock()")
	p.P("defer s.mu.Unlock()")
	p.P("if wsFrameSize(data, raw) > wsChunkThreshold {")
	p.P("return wsWriteChunked(func() (io.WriteCloser, error) { return s.conn.Writer(ctx, websocket.MessageBinary) }, binaryFrame, append([][]byte{data}, raw...))")
	p.P("}")
	p.P("if raw != nil {")
	p.P("w, err := s.conn.Writer(ctx, websocket.MessageBinary)")
	p.P("if err != nil { return err }")
	p.P("return wsWriteParts(w, data, raw)")
	p.P("}")
	p.P("return s.conn.Write(ctx, typ, data)")
	p.P("}")
	p.P()
}

// writeWSPendingType emits the generic correlation-map runtime shared by
// every @ws_id-using method and duplex type in the file: register(id) hands
// back a channel that resolve(id, value) fulfills exactly once, so a
// concurrent Call can await a specific reply among many interleaved frames.
//
// typeName/constructorName are parameterized because a project's standard
// layout puts client.gen.go and server.gen.go in the same package (see
// examples/onk-simple-api/api): the client and server sides each emit their
// own copy under distinct names so the two files never collide, even though
// every other detail of the type is identical.
func writeWSPendingType(p *Printer, typeName, constructorName, closedErrName string) {
	p.P("// ", closedErrName, " is what a correlated Call returns once the connection is")
	p.P("// gone. It matches errors.Is(err, net.ErrClosed), and errors.As reaches the")
	p.P("// underlying read error (e.g. a websocket.CloseError carrying")
	p.P("// StatusMessageTooBig) when there is one.")
	p.P("type ", closedErrName, " struct{ cause error }")
	p.P()
	p.P("func (e *", closedErrName, ") Error() string {")
	p.P(`if e.cause == nil { return "websocket closed" }`)
	p.P(`return "websocket closed: " + e.cause.Error()`)
	p.P("}")
	p.P()
	p.P("func (e *", closedErrName, ") Unwrap() []error {")
	p.P("if e.cause == nil { return []error{net.ErrClosed} }")
	p.P("return []error{net.ErrClosed, e.cause}")
	p.P("}")
	p.P()
	p.P("// ", typeName, " tracks in-flight correlated WebSocket calls, keyed by an")
	p.P("// application-supplied @ws_id value, so multiple calls can be")
	p.P("// outstanding at once on a single connection and resolved out of order.")
	p.P("// ", typeName, "Waiter is one in-flight call: the reply channel, and the")
	p.P("// oneof variant tag of the frame the call sent (\"\" when not a oneof).")
	p.P("type ", typeName, "Waiter[T any] struct {")
	p.P("ch chan T")
	p.P("sent string")
	p.P("}")
	p.P()
	p.P("type ", typeName, "[K comparable, T any] struct {")
	p.P("mu sync.Mutex")
	p.P("waiters map[K]", typeName, "Waiter[T]")
	p.P("// err is set once, by closeAll, and is sticky.")
	p.P("err error")
	p.P("}")
	p.P()
	p.P("func ", constructorName, "[K comparable, T any]() *", typeName, "[K, T] {")
	p.P("return &", typeName, "[K, T]{waiters: make(map[K]", typeName, "Waiter[T])}")
	p.P("}")
	p.P()
	p.P("// register fails once closeAll has run, so a Call made after the")
	p.P("// connection is gone fails immediately rather than waiting on a reply the")
	p.P("// read loop will never deliver.")
	p.P("func (p *", typeName, "[K, T]) register(id K, sent string) (chan T, error) {")
	p.P("p.mu.Lock()")
	p.P("defer p.mu.Unlock()")
	p.P("if p.err != nil { return nil, p.err }")
	p.P("ch := make(chan T, 1)")
	p.P("p.waiters[id] = ", typeName, "Waiter[T]{ch: ch, sent: sent}")
	p.P("return ch, nil")
	p.P("}")
	p.P()
	p.P("// resolve hands value to the call waiting on id, unless value is the same")
	p.P("// oneof variant that call sent: that is the peer starting its own call")
	p.P("// under a colliding id, not a reply, so it goes to the handler/Receive.")
	p.P("func (p *", typeName, "[K, T]) resolve(id K, variant string, value T) bool {")
	p.P("p.mu.Lock()")
	p.P("w, ok := p.waiters[id]")
	p.P(`if ok && w.sent != "" && w.sent == variant { ok = false }`)
	p.P("if ok { delete(p.waiters, id) }")
	p.P("p.mu.Unlock()")
	p.P("if ok { w.ch <- value }")
	p.P("return ok")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) cancel(id K) {")
	p.P("p.mu.Lock()")
	p.P("delete(p.waiters, id)")
	p.P("p.mu.Unlock()")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) closedErr() error {")
	p.P("p.mu.Lock()")
	p.P("defer p.mu.Unlock()")
	p.P("return p.err")
	p.P("}")
	p.P()
	p.P("// closeAll fails every in-flight call with cause; only the first call's")
	p.P("// cause is kept.")
	p.P("func (p *", typeName, "[K, T]) closeAll(cause error) {")
	p.P("p.mu.Lock()")
	p.P("if p.err != nil { p.mu.Unlock(); return }")
	p.P("p.err = &", closedErrName, "{cause: cause}")
	p.P("waiters := p.waiters")
	p.P("p.waiters = make(map[K]", typeName, "Waiter[T])")
	p.P("p.mu.Unlock()")
	p.P("for _, w := range waiters { close(w.ch) }")
	p.P("}")
	p.P()
}

// wsClientPendingType/wsServerPendingType are the distinct per-file names
// writeWSPendingType is instantiated under; see its doc comment for why.
const (
	wsClientPendingType        = "wsPending"
	wsClientPendingConstructor = "newWSPending"
	wsClientClosedError        = "wsClosedError"
	wsServerPendingType        = "wsServerPending"
	wsServerPendingConstructor = "newWSServerPending"
	wsServerClosedError        = "wsServerClosedError"
)

// defaultMaxWSFrameBytes is the inbound message cap every target applies
// unless configured otherwise (Go's websocket library alone would default to
// 32 KiB, TS's ws to 100 MiB, Rust's tungstenite to 64 MiB).
const defaultMaxWSFrameBytes = "16 << 20"

// writeWSCallAwait emits the tail of a correlated Call: wait for the reply,
// the connection closing, or ctx. On ctx, the waiter is dropped and - when the
// schema declares a @ws_cancel variant for the frames this side sends - the
// peer is told, so it can stop working on id.
func writeWSCallAwait(p *Printer, recv string, sent *onkir.Message) {
	p.P("select {")
	p.P("case result, ok := <-ch:")
	p.P("if !ok { return nil, ", recv, ".pending.closedErr() }")
	p.P("return result, nil")
	p.P("case <-ctx.Done():")
	p.P(recv, ".pending.cancel(id)")
	if frame, ok := goWSCancelFrame(p, sent, "id"); ok {
		p.P("// Best effort, on a fresh deadline: ctx is already done.")
		p.P("cancelCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)")
		p.P("_ = ", recv, ".Send(cancelCtx, ", frame, ")")
		p.P("stop()")
	}
	p.P("return nil, ctx.Err()")
	p.P("}")
}

// goWSCancelFrame builds the Go expression for message's @ws_cancel frame
// carrying idVar, if the schema declares one.
func goWSCancelFrame(p *Printer, message *onkir.Message, idVar string) (string, bool) {
	oneofField, variant, idField, ok := onkir.WSCancelVariant(message)
	if !ok {
		return "", false
	}
	idValue := idVar
	if idField.Optional {
		idValue = "&" + idVar
	}
	return "&" + p.MessageTypeName(message) + "{" + PascalCase(oneofField.Name) + ": &" +
		OneofVariantTypeName(message, oneofField, variant) + "{" + PascalCase(variant.Name) + ": &" +
		p.MessageTypeName(variant.Type.Message) + "{" + PascalCase(idField.Name) + ": " + idValue + "}}}", true
}

// wsOutName returns the per-method concrete out type name generated for a
// @ws method that uses @ws_id, in place of the shared WSOut[E] interface.
func wsOutName(s *onkir.Service, m *onkir.Method) string {
	return PascalCase(s.Name) + PascalCase(m.Name) + "Out"
}

// writeWSCorrelatedOutType emits the per-method concrete send type used when
// a @ws method's request or response (directly, or via a oneof variant)
// carries @ws_id: Send behaves like wsConnOut, and Call additionally
// registers a pending waiter, sends, and blocks for the matching reply that
// the read loop resolves once it arrives.
func writeWSCorrelatedOutType(p *Printer, s *onkir.Service, m *onkir.Method, idField *onkir.Field) {
	name := wsOutName(s, m)
	reqRef := p.MessageTypeName(m.Request)
	resRef := p.MessageTypeName(m.Response)
	idType := p.GoFieldType(idField.Type)

	p.P("type ", name, " struct {")
	p.P("conn *websocket.Conn")
	p.P("ctx context.Context")
	p.P("mu sync.Mutex")
	p.P("pending *", wsServerPendingType, "[", idType, ", *", reqRef, "]")
	p.P("}")
	p.P()
	p.P("func (o *", name, ") Context() context.Context { return o.ctx }")
	p.P()
	p.P("func (o *", name, ") Close(code websocket.StatusCode, reason string) error { return o.conn.Close(code, wsCloseReason(reason)) }")
	p.P()
	p.P("func (o *", name, ") Send(ctx context.Context, value *", resRef, ") error {")
	p.P("buf := wsGetBuffer()")
	p.P("defer wsPutBuffer(buf)")
	p.P("binaryFrame, data, raw, err := wsEncodeAppend((*buf)[:0], value)")
	p.P("if cap(data) > 0 { *buf = data[:0] }")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("typ := websocket.MessageText")
	p.P("if binaryFrame { typ = websocket.MessageBinary }")
	p.P("o.mu.Lock()")
	p.P("defer o.mu.Unlock()")
	p.P("if wsFrameSize(data, raw) > wsChunkThreshold {")
	p.P("return wsWriteChunked(func() (io.WriteCloser, error) { return o.conn.Writer(ctx, websocket.MessageBinary) }, binaryFrame, append([][]byte{data}, raw...))")
	p.P("}")
	p.P("if raw != nil {")
	p.P("w, err := o.conn.Writer(ctx, websocket.MessageBinary)")
	p.P("if err != nil { return err }")
	p.P("return wsWriteParts(w, data, raw)")
	p.P("}")
	p.P("return o.conn.Write(ctx, typ, data)")
	p.P("}")
	p.P()
	p.P("// Call sends value, then blocks until a request-direction frame")
	p.P("// carrying the matching @ws_id arrives (resolved by the read loop),")
	p.P("// ctx is done, or the connection closes. Errors match")
	p.P("// errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded)")
	p.P("// or errors.Is(err, net.ErrClosed) respectively.")
	p.P("func (o *", name, ") Call(ctx context.Context, id ", idType, ", value *", resRef, ") (*", reqRef, ", error) {")
	writeWSDeadlineStamp(p, m.Response)
	writeWSVariantTag(p, "value", m.Response, "sent")
	p.P("ch, err := o.pending.register(id, sent)")
	p.P("if err != nil { return nil, err }")
	p.P("if err := o.Send(ctx, value); err != nil {")
	p.P("o.pending.cancel(id)")
	p.P("return nil, err")
	p.P("}")
	writeWSCallAwait(p, "o", m.Response)
	p.P("}")
	p.P()
}

// writeWSDuplexType emits the client-side duplex handle returned by
// WebSocket client methods. With no correlation field it's the original
// explicit Send/Receive/Close, mutex-guarded on Send. With one, a background
// reader is started lazily so Call (awaiting one correlated reply) and
// Receive (reading every other inbound frame) can be used concurrently
// without racing on the same connection.
func writeWSDuplexType(p *Printer, inName, outName string, idField *onkir.Field, reqMessage, respMessage *onkir.Message) {
	name := wsDuplexName(inName, outName)
	p.P("// ", name, " is a bidirectional WebSocket connection:")
	p.P("type ", name, " struct {")
	p.P("conn *websocket.Conn")
	p.P("mu sync.Mutex")
	p.P("readBuf []byte")
	p.P("readHint int")
	p.P("asm wsAssembler")
	if idField != nil {
		idType := p.GoFieldType(idField.Type)
		p.P("pending *", wsClientPendingType, "[", idType, ", *", outName, "]")
		p.P("inbox chan *", outName)
		p.P("readErr chan error")
		p.P("readOnce sync.Once")
		p.P("pingInterval time.Duration")
	}
	p.P("}")
	p.P()
	p.P("func (d *", name, ") Send(ctx context.Context, value *", inName, ") error {")
	p.P("if validator, ok := any(value).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return fmt.Errorf(\"validate frame: %w\", err) } }")
	p.P("buf := wsGetBuffer()")
	p.P("defer wsPutBuffer(buf)")
	p.P("binaryFrame, data, raw, err := wsEncodeAppend((*buf)[:0], value)")
	p.P("if cap(data) > 0 { *buf = data[:0] }")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("typ := websocket.MessageText")
	p.P("if binaryFrame { typ = websocket.MessageBinary }")
	p.P("d.mu.Lock()")
	p.P("defer d.mu.Unlock()")
	p.P("if wsFrameSize(data, raw) > wsChunkThreshold {")
	p.P("return wsWriteChunked(func() (io.WriteCloser, error) { return d.conn.Writer(ctx, websocket.MessageBinary) }, binaryFrame, append([][]byte{data}, raw...))")
	p.P("}")
	p.P("if raw != nil {")
	p.P("w, err := d.conn.Writer(ctx, websocket.MessageBinary)")
	p.P("if err != nil { return err }")
	p.P("return wsWriteParts(w, data, raw)")
	p.P("}")
	p.P("return d.conn.Write(ctx, typ, data)")
	p.P("}")
	p.P()

	if idField == nil {
		p.P("func (d *", name, ") Receive(ctx context.Context) (*", outName, ", error) {")
		p.P("for {")
		writeWSBufferedRead(p, "d.conn", "ctx", "d.readBuf", "d.readHint")
		p.P("if err != nil { return nil, wsReadError(err) }")
		writeWSAssemble(p, "d.asm", "d.readBuf", "d.readHint")
		p.P("if feedErr != nil {")
		p.P(`_ = d.conn.Close(websocket.StatusCode(wsChunkCloseCode(feedErr)), "invalid chunked message")`)
		p.P("return nil, feedErr")
		p.P("}")
		p.P("if !complete { continue }")
		p.P("frame := new(", outName, ")")
		p.P("if err := wsDecode(msgBinary, msg, frame); err != nil { return nil, fmt.Errorf(\"decode frame: %w\", err) }")
		p.P("return frame, nil")
		p.P("}")
		p.P("}")
		p.P()
		p.P("func (d *", name, ") Close() error { return d.conn.Close(websocket.StatusNormalClosure, \"\") }")
		p.P()
		return
	}

	writeWSCorrelatedDuplexMethods(p, name, inName, outName, idField, reqMessage, respMessage)
}

// writeWSCorrelatedDuplexMethods emits the ensureReader/readLoop/Receive/Call
// methods for a @ws_id-using duplex type, split out of writeWSDuplexType to
// keep both functions under the linter's statement-count limit.
func writeWSCorrelatedDuplexMethods(p *Printer, name, inName, outName string, idField *onkir.Field, reqMessage, respMessage *onkir.Message) {
	idType := p.GoFieldType(idField.Type)

	p.P("func (d *", name, ") ensureReader() {")
	p.P("d.readOnce.Do(func() {")
	p.P("d.inbox = make(chan *", outName, ", 16)")
	p.P("d.readErr = make(chan error, 1)")
	p.P("d.pending = ", wsClientPendingConstructor, "[", idType, ", *", outName, "]()")
	p.P("ctx, stop := context.WithCancel(context.Background())")
	p.P("go wsKeepAlive(ctx, d.conn.Ping, d.conn.CloseNow, d.pingInterval)")
	p.P("go d.readLoop(stop)")
	p.P("})")
	p.P("}")
	p.P()
	p.P("func (d *", name, ") readLoop(stop context.CancelFunc) {")
	p.P("defer stop()")
	p.P("for {")
	writeWSBufferedRead(p, "d.conn", "context.Background()", "d.readBuf", "d.readHint")
	p.P("if err != nil {")
	p.P("err = wsReadError(err)")
	p.P("d.pending.closeAll(err)")
	p.P("d.readErr <- err")
	p.P("close(d.inbox)")
	p.P("return")
	p.P("}")
	writeWSAssemble(p, "d.asm", "d.readBuf", "d.readHint")
	p.P("if feedErr != nil {")
	p.P(`_ = d.conn.Close(websocket.StatusCode(wsChunkCloseCode(feedErr)), "invalid chunked message")`)
	p.P("d.pending.closeAll(feedErr)")
	p.P("d.readErr <- feedErr")
	p.P("close(d.inbox)")
	p.P("return")
	p.P("}")
	p.P("if !complete { continue }")
	p.P("frame := new(", outName, ")")
	p.P("decodeErr := wsDecode(msgBinary, msg, frame)")
	p.P("if decodeErr != nil {")
	p.P(`err = fmt.Errorf("decode frame: %w", decodeErr)`)
	p.P(`_ = d.conn.Close(websocket.StatusInvalidFramePayloadData, "invalid frame")`)
	p.P("d.pending.closeAll(err)")
	p.P("d.readErr <- err")
	p.P("close(d.inbox)")
	p.P("return")
	p.P("}")
	writeWSIDExtraction(p, "frame", respMessage, idField, "id", "idOk", "variant")
	p.P("if idOk && d.pending.resolve(id, variant, frame) { continue }")
	p.P("d.inbox <- frame")
	p.P("}")
	p.P("}")
	p.P()
	p.P("func (d *", name, ") Receive(ctx context.Context) (*", outName, ", error) {")
	p.P("d.ensureReader()")
	p.P("select {")
	p.P("case frame, ok := <-d.inbox:")
	p.P("if !ok {")
	p.P("select {")
	p.P("case err := <-d.readErr:")
	p.P("return nil, err")
	p.P("default:")
	p.P("return nil, io.EOF")
	p.P("}")
	p.P("}")
	p.P("return frame, nil")
	p.P("case <-ctx.Done():")
	p.P("return nil, ctx.Err()")
	p.P("}")
	p.P("}")
	p.P()
	p.P("// Call sends value, then blocks until a response-direction frame")
	p.P("// carrying the matching @ws_id arrives, ctx is done, or the")
	p.P("// connection closes. Safe to use alongside Receive: the background")
	p.P("// reader routes correlated replies here and everything else to it.")
	p.P("// Errors match errors.Is(err, context.Canceled),")
	p.P("// errors.Is(err, context.DeadlineExceeded) or errors.Is(err, net.ErrClosed).")
	p.P("func (d *", name, ") Call(ctx context.Context, id ", idType, ", value *", inName, ") (*", outName, ", error) {")
	p.P("d.ensureReader()")
	writeWSDeadlineStamp(p, reqMessage)
	writeWSVariantTag(p, "value", reqMessage, "sent")
	p.P("ch, err := d.pending.register(id, sent)")
	p.P("if err != nil { return nil, err }")
	p.P("if err := d.Send(ctx, value); err != nil {")
	p.P("d.pending.cancel(id)")
	p.P("return nil, err")
	p.P("}")
	writeWSCallAwait(p, "d", reqMessage)
	p.P("}")
	p.P()
	p.P("func (d *", name, ") Close() error { return d.conn.Close(websocket.StatusNormalClosure, \"\") }")
	p.P()
}

// writeWSIDExtraction emits statements declaring idVar/okVar and setting
// them from frameVar (a *message) from whichever field of message actually
// carries @ws_id - a direct field, or (independently, per variant) any
// oneof variant whose own message carries one. idField only supplies the
// shared Go type for idVar: onkcompile guarantees every @ws_id field a
// method touches shares one scalar type, but each oneof variant has its
// own distinct field (e.g. HostCall.Id vs HostResult.Id) with its own name
// and optionality, so - unlike an earlier version of this function - it
// must not filter variants by comparing against idField's identity: doing
// so only ever matched whichever single field onkir.WSIDField(message)
// happened to return first (declaration order), silently generating no
// extraction code at all for every other variant's @ws_id field.
func writeWSIDExtraction(p *Printer, frameVar string, message *onkir.Message, idField *onkir.Field, idVar, okVar, tagVar string) {
	idType := p.GoFieldType(idField.Type)
	p.P("var ", idVar, " ", idType)
	p.P("var ", okVar, " bool")
	p.P("var ", tagVar, " string")
	if message == nil {
		return
	}
	emitReturn := func(field *onkir.Field, accessor string) {
		if field.Optional && field.Type.Kind != onkir.KindMessage {
			p.P("if ", accessor, " != nil { ", idVar, ", ", okVar, " = *", accessor, ", true }")
			return
		}
		p.P(idVar, ", ", okVar, " = ", accessor, ", true")
	}
	for _, f := range message.Fields {
		if f.Oneof != nil {
			for _, variant := range f.Oneof.Variants {
				// A cancel is never a reply: it goes to the handler/Receive.
				if variant.IsWSCancel() || variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
					continue
				}
				vf, ok := onkir.WSIDField(variant.Type.Message)
				if !ok {
					continue
				}
				typeName := OneofVariantTypeName(message, f, variant)
				variantAccessor := "v." + PascalCase(variant.Name)
				fieldAccessor := variantAccessor + "." + PascalCase(vf.Name)
				p.P("if v, ok := ", frameVar, ".Get", PascalCase(f.Name), "().(*", typeName, "); ok && v != nil && ", variantAccessor, " != nil {")
				emitReturn(vf, fieldAccessor)
				p.P(tagVar, " = ", fmt.Sprintf("%q", variant.Tag()))
				p.P("}")
			}
			continue
		}
		if !f.HasDecorator("ws_id") {
			continue
		}
		emitReturn(f, frameVar+"."+PascalCase(f.Name))
	}
}

// writeWSVariantTag declares tagVar as the oneof variant tag of frameVar -
// among the variants that carry @ws_id, the only ones a call can send - or
// "" when message correlates on a direct field instead.
func writeWSVariantTag(p *Printer, frameVar string, message *onkir.Message, tagVar string) {
	p.P("var ", tagVar, " string")
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, variant := range f.Oneof.Variants {
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
				continue
			}
			if _, ok := onkir.WSIDField(variant.Type.Message); !ok {
				continue
			}
			p.P("if _, ok := ", frameVar, ".Get", PascalCase(f.Name), "().(*", OneofVariantTypeName(message, f, variant), "); ok { ", tagVar, " = ", fmt.Sprintf("%q", variant.Tag()), " }")
		}
	}
}

func wsDuplexName(inName, outName string) string {
	return inName + "To" + outName + "Socket"
}

// --- client ---------------------------------------------------------------

func writeWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.WebSocketPath()
	fullPath := s.BasePath + path
	reqRef := p.MessageTypeName(m.Request)
	resRef := p.MessageTypeName(m.Response)

	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name),
		"(ctx context.Context, req *", reqRef, ") (*", wsDuplexName(reqRef, resRef), ", error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return nil, fmt.Errorf("validate request: %w", err) } }`)

	p.P("path := ", fmt.Sprintf("%q", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		p.P("path = strings.ReplaceAll(path, ", fmt.Sprintf("%q", "{"+paramName+"}"), ", ",
			fmt.Sprintf("url.PathEscape(fmt.Sprintf(%q, req.%s))", "%v", PascalCase(paramName)), ")")
	}
	writeClientQueryParams(p, m.Request)

	// http(s) base URLs upgrade to ws(s).
	p.P(`socketURL := c.BaseURL + path`)
	p.P(`if strings.HasPrefix(socketURL, "https://") { socketURL = "wss://" + strings.TrimPrefix(socketURL, "https://") } else if strings.HasPrefix(socketURL, "http://") { socketURL = "ws://" + strings.TrimPrefix(socketURL, "http://") }`)
	p.P("header := http.Header{}")
	p.P("for key, value := range c.Headers { header.Set(key, value) }")
	p.P("conn, _, err := websocket.Dial(ctx, socketURL, &websocket.DialOptions{ HTTPClient: c.HTTPClient, HTTPHeader: header })")
	p.P("if err != nil { return nil, fmt.Errorf(\"dial websocket: %w\", err) }")
	p.P("conn.SetReadLimit(wsReadLimit(c.MaxWSFrameBytes))")
	if _, correlated := m.WSIDField(); correlated {
		p.P("return &", wsDuplexName(reqRef, resRef), "{conn: conn, asm: wsAssembler{limit: c.MaxWSMessageBytes}, pingInterval: wsPingInterval(c.WSPingInterval)}, nil")
	} else {
		p.P("return &", wsDuplexName(reqRef, resRef), "{conn: conn, asm: wsAssembler{limit: c.MaxWSMessageBytes}}, nil")
	}
	p.P("}")
	p.P()
}

// --- server ---------------------------------------------------------------

func writeWSRoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.WebSocketPath()
	fullPath := s.BasePath + path
	resRef := p.MessageTypeName(m.Response)
	idField, correlated := m.WSIDField()

	p.P("mux.Handle(", fmt.Sprintf("%q", "GET "+fullPath), ", o.wrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {")
	p.P("req := new(", p.MessageTypeName(m.Request), ")")

	writePathParamBinding(p, fullPath, m.Request)
	writeQueryParamBinding(p, m.Request)

	for _, h := range m.Service.Headers {
		writeHeaderCheck(p, h)
	}
	for _, h := range m.Headers {
		writeHeaderCheck(p, h)
	}
	writeValidateCall(p)

	p.P("conn, err := websocket.Accept(w, r, nil)")
	p.P("if err != nil { return }")
	p.P("defer conn.CloseNow()")
	p.P("conn.SetReadLimit(wsServerReadLimit(o.maxWSFrameBytes))")
	p.P("ctx := r.Context()")
	p.P("connCtx, closeConn := context.WithCancelCause(ctx)")
	p.P("defer closeConn(net.ErrClosed)")
	p.P("go wsKeepAlive(connCtx, conn.Ping, conn.CloseNow, wsPingInterval(o.wsPingInterval))")
	if correlated {
		idType := p.GoFieldType(idField.Type)
		p.P("out := &", wsOutName(s, m), "{conn: conn, ctx: connCtx, pending: ", wsServerPendingConstructor, "[", idType, ", *", p.MessageTypeName(m.Request), "]()}")
		p.P("defer out.pending.closeAll(nil)")
	} else {
		p.P("out := &wsConnOut[", resRef, "]{conn: conn, ctx: connCtx}")
	}
	p.P("// Failures end the connection with a close code and the message as the")
	p.P("// reason, never an off-schema frame: 1007 for a frame that does not decode")
	p.P("// or validate, 1011 for a handler error.")
	p.P("sendProtocolError := func(message string) {")
	p.P("_ = conn.Close(websocket.StatusInvalidFramePayloadData, wsCloseReason(message))")
	p.P("}")
	p.P("var readBuf []byte")
	p.P("readHint := 0")
	p.P("asm := wsAssembler{limit: o.maxWSMessageBytes}")
	p.P("for {")
	writeWSBufferedRead(p, "conn", "ctx", "readBuf", "readHint")
	if correlated {
		p.P("if err != nil { closeConn(err); out.pending.closeAll(err); return }")
	} else {
		p.P("if err != nil { closeConn(err); return }")
	}
	writeWSAssemble(p, "asm", "readBuf", "readHint")
	p.P("if feedErr != nil {")
	p.P("_ = conn.Close(websocket.StatusCode(wsChunkCloseCode(feedErr)), wsCloseReason(feedErr.Error()))")
	p.P("return")
	p.P("}")
	p.P("if !complete { continue }")
	p.P("frame := new(", p.MessageTypeName(m.Request), ")")
	p.P("decodeErr := wsDecode(msgBinary, msg, frame)")
	p.P("if decodeErr != nil {")
	p.P(`sendProtocolError("invalid JSON frame")`)
	p.P("return")
	p.P("}")
	p.P("if validator, ok := any(frame).(interface{ Validate() error }); ok { if verr := validator.Validate(); verr != nil {")
	p.P(`sendProtocolError(verr.Error())`)
	p.P("return")
	p.P("} }")
	if correlated {
		writeWSIDExtraction(p, "frame", m.Request, idField, "replyID", "replyIDOk", "replyVariant")
		p.P("if replyIDOk && out.pending.resolve(replyID, replyVariant, frame) { continue }")
	}
	p.P("if err := srv.", PascalCase(m.Name), "(ctx, frame, out); err != nil {")
	p.P("_ = conn.Close(websocket.StatusInternalError, wsCloseReason(err.Error()))")
	p.P("return")
	p.P("}")
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", "GET"), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), "}))")
}

var _ = strings.ToUpper // reserved for future verb normalization in WS metadata

func writeWSBufferedRead(p *Printer, conn, ctx, buf, hint string) {
	p.P("typ, reader, err := ", conn, ".Reader(", ctx, ")")
	p.P("var data []byte")
	p.P("if err == nil {")
	p.P("if ", buf, " == nil { ", buf, " = make([]byte, 0, max(", hint, ", 4096)) }")
	p.P("data, err = wsReadAll(reader, ", buf, "[:0])")
	p.P("}")
}

func writeWSAssemble(p *Printer, asm, buf, hint string) {
	p.P("msgBinary, msg, complete, aliased, feedErr := ", asm, ".feed(typ == websocket.MessageBinary, data)")
	p.P(hint, " = len(data)")
	p.P("if aliased && msgBinary || cap(data) > 1<<20 { ", buf, " = nil } else { ", buf, " = data[:0] }")
}
