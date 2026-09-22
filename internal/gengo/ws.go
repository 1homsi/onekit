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
	p.P("}")
	p.P()
	p.P("type wsConnOut[E any] struct {")
	p.P("conn *websocket.Conn")
	p.P("mu sync.Mutex")
	p.P("}")
	p.P()
	p.P("func (s *wsConnOut[E]) Send(ctx context.Context, value *E) error {")
	p.P("data, err := json.Marshal(value)")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("s.mu.Lock()")
	p.P("defer s.mu.Unlock()")
	p.P("return s.conn.Write(ctx, websocket.MessageText, data)")
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
func writeWSPendingType(p *Printer, typeName, constructorName string) {
	p.P("// ", typeName, " tracks in-flight correlated WebSocket calls, keyed by an")
	p.P("// application-supplied @ws_id value, so multiple calls can be")
	p.P("// outstanding at once on a single connection and resolved out of order.")
	p.P("type ", typeName, "[K comparable, T any] struct {")
	p.P("mu sync.Mutex")
	p.P("waiters map[K]chan T")
	p.P("}")
	p.P()
	p.P("func ", constructorName, "[K comparable, T any]() *", typeName, "[K, T] {")
	p.P("return &", typeName, "[K, T]{waiters: make(map[K]chan T)}")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) register(id K) chan T {")
	p.P("ch := make(chan T, 1)")
	p.P("p.mu.Lock()")
	p.P("p.waiters[id] = ch")
	p.P("p.mu.Unlock()")
	p.P("return ch")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) resolve(id K, value T) bool {")
	p.P("p.mu.Lock()")
	p.P("ch, ok := p.waiters[id]")
	p.P("if ok { delete(p.waiters, id) }")
	p.P("p.mu.Unlock()")
	p.P("if ok { ch <- value }")
	p.P("return ok")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) cancel(id K) {")
	p.P("p.mu.Lock()")
	p.P("delete(p.waiters, id)")
	p.P("p.mu.Unlock()")
	p.P("}")
	p.P()
	p.P("func (p *", typeName, "[K, T]) closeAll() {")
	p.P("p.mu.Lock()")
	p.P("waiters := p.waiters")
	p.P("p.waiters = make(map[K]chan T)")
	p.P("p.mu.Unlock()")
	p.P("for _, ch := range waiters { close(ch) }")
	p.P("}")
	p.P()
}

// wsClientPendingType/wsServerPendingType are the distinct per-file names
// writeWSPendingType is instantiated under; see its doc comment for why.
const (
	wsClientPendingType        = "wsPending"
	wsClientPendingConstructor = "newWSPending"
	wsServerPendingType        = "wsServerPending"
	wsServerPendingConstructor = "newWSServerPending"
)

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
	p.P("mu sync.Mutex")
	p.P("pending *", wsServerPendingType, "[", idType, ", *", reqRef, "]")
	p.P("}")
	p.P()
	p.P("func (o *", name, ") Send(ctx context.Context, value *", resRef, ") error {")
	p.P("data, err := json.Marshal(value)")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("o.mu.Lock()")
	p.P("defer o.mu.Unlock()")
	p.P("return o.conn.Write(ctx, websocket.MessageText, data)")
	p.P("}")
	p.P()
	p.P("// Call sends value, then blocks until a request-direction frame")
	p.P("// carrying the matching @ws_id arrives (resolved by the read loop),")
	p.P("// ctx is done, or the connection closes.")
	p.P("func (o *", name, ") Call(ctx context.Context, id ", idType, ", value *", resRef, ") (*", reqRef, ", error) {")
	p.P("ch := o.pending.register(id)")
	p.P("if err := o.Send(ctx, value); err != nil {")
	p.P("o.pending.cancel(id)")
	p.P("return nil, err")
	p.P("}")
	p.P("select {")
	p.P("case result, ok := <-ch:")
	p.P("if !ok { return nil, fmt.Errorf(\"websocket closed while awaiting reply\") }")
	p.P("return result, nil")
	p.P("case <-ctx.Done():")
	p.P("o.pending.cancel(id)")
	p.P("return nil, ctx.Err()")
	p.P("}")
	p.P("}")
	p.P()
}

// writeWSDuplexType emits the client-side duplex handle returned by
// WebSocket client methods. With no correlation field it's the original
// explicit Send/Receive/Close, mutex-guarded on Send. With one, a background
// reader is started lazily so Call (awaiting one correlated reply) and
// Receive (reading every other inbound frame) can be used concurrently
// without racing on the same connection.
func writeWSDuplexType(p *Printer, inName, outName string, idField *onkir.Field, respMessage *onkir.Message) {
	name := wsDuplexName(inName, outName)
	p.P("// ", name, " is a bidirectional WebSocket connection:")
	p.P("type ", name, " struct {")
	p.P("conn *websocket.Conn")
	p.P("mu sync.Mutex")
	if idField != nil {
		idType := p.GoFieldType(idField.Type)
		p.P("pending *", wsClientPendingType, "[", idType, ", *", outName, "]")
		p.P("inbox chan *", outName)
		p.P("readErr chan error")
		p.P("readOnce sync.Once")
	}
	p.P("}")
	p.P()
	p.P("func (d *", name, ") Send(ctx context.Context, value *", inName, ") error {")
	p.P("if validator, ok := any(value).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return fmt.Errorf(\"validate frame: %w\", err) } }")
	p.P("data, err := json.Marshal(value)")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("d.mu.Lock()")
	p.P("defer d.mu.Unlock()")
	p.P("return d.conn.Write(ctx, websocket.MessageText, data)")
	p.P("}")
	p.P()

	if idField == nil {
		p.P("func (d *", name, ") Receive(ctx context.Context) (*", outName, ", error) {")
		p.P("_, data, err := d.conn.Read(ctx)")
		p.P("if err != nil { return nil, err }")
		p.P("frame := new(", outName, ")")
		p.P("if err := json.Unmarshal(data, frame); err != nil { return nil, fmt.Errorf(\"decode frame: %w\", err) }")
		p.P("return frame, nil")
		p.P("}")
		p.P()
		p.P("func (d *", name, ") Close() error { return d.conn.Close(websocket.StatusNormalClosure, \"\") }")
		p.P()
		return
	}

	writeWSCorrelatedDuplexMethods(p, name, inName, outName, idField, respMessage)
}

// writeWSCorrelatedDuplexMethods emits the ensureReader/readLoop/Receive/Call
// methods for a @ws_id-using duplex type, split out of writeWSDuplexType to
// keep both functions under the linter's statement-count limit.
func writeWSCorrelatedDuplexMethods(p *Printer, name, inName, outName string, idField *onkir.Field, respMessage *onkir.Message) {
	idType := p.GoFieldType(idField.Type)

	p.P("func (d *", name, ") ensureReader() {")
	p.P("d.readOnce.Do(func() {")
	p.P("d.inbox = make(chan *", outName, ", 16)")
	p.P("d.readErr = make(chan error, 1)")
	p.P("d.pending = ", wsClientPendingConstructor, "[", idType, ", *", outName, "]()")
	p.P("go d.readLoop()")
	p.P("})")
	p.P("}")
	p.P()
	p.P("func (d *", name, ") readLoop() {")
	p.P("for {")
	p.P("_, data, err := d.conn.Read(context.Background())")
	p.P("if err != nil {")
	p.P("d.pending.closeAll()")
	p.P("d.readErr <- err")
	p.P("close(d.inbox)")
	p.P("return")
	p.P("}")
	p.P("frame := new(", outName, ")")
	p.P("if err := json.Unmarshal(data, frame); err != nil { continue }")
	writeWSIDExtraction(p, "frame", respMessage, idField, "id", "idOk")
	p.P("if idOk && d.pending.resolve(id, frame) { continue }")
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
	p.P("func (d *", name, ") Call(ctx context.Context, id ", idType, ", value *", inName, ") (*", outName, ", error) {")
	p.P("d.ensureReader()")
	p.P("ch := d.pending.register(id)")
	p.P("if err := d.Send(ctx, value); err != nil {")
	p.P("d.pending.cancel(id)")
	p.P("return nil, err")
	p.P("}")
	p.P("select {")
	p.P("case result, ok := <-ch:")
	p.P("if !ok { return nil, fmt.Errorf(\"websocket closed while awaiting reply\") }")
	p.P("return result, nil")
	p.P("case <-ctx.Done():")
	p.P("d.pending.cancel(id)")
	p.P("return nil, ctx.Err()")
	p.P("}")
	p.P("}")
	p.P()
	p.P("func (d *", name, ") Close() error { return d.conn.Close(websocket.StatusNormalClosure, \"\") }")
	p.P()
}

// writeWSIDExtraction emits statements declaring idVar/okVar and setting
// them from frameVar (a *message) when message carries idField either
// directly or within one of its oneof variants' own messages.
func writeWSIDExtraction(p *Printer, frameVar string, message *onkir.Message, idField *onkir.Field, idVar, okVar string) {
	idType := p.GoFieldType(idField.Type)
	p.P("var ", idVar, " ", idType)
	p.P("var ", okVar, " bool")
	if message == nil {
		return
	}
	directAccess := func(accessor string) (string, bool) {
		if idField.Optional && idField.Type.Kind != onkir.KindMessage {
			return accessor, true
		}
		return accessor, false
	}
	for _, f := range message.Fields {
		if f.Oneof != nil {
			for _, variant := range f.Oneof.Variants {
				if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
					continue
				}
				vf, ok := onkir.WSIDField(variant.Type.Message)
				if !ok || vf != idField {
					continue
				}
				typeName := OneofVariantTypeName(message, f, variant)
				variantAccessor := "v." + PascalCase(variant.Name)
				fieldAccessor := variantAccessor + "." + PascalCase(idField.Name)
				p.P("if v, ok := ", frameVar, ".Get", PascalCase(f.Name), "().(*", typeName, "); ok && v != nil && ", variantAccessor, " != nil {")
				if accessor, optional := directAccess(fieldAccessor); optional {
					p.P("if ", accessor, " != nil { ", idVar, ", ", okVar, " = *", accessor, ", true }")
				} else {
					p.P(idVar, ", ", okVar, " = ", accessor, ", true")
				}
				p.P("}")
			}
			continue
		}
		if f != idField {
			continue
		}
		accessor := frameVar + "." + PascalCase(idField.Name)
		if accessor2, optional := directAccess(accessor); optional {
			p.P("if ", accessor2, " != nil { ", idVar, ", ", okVar, " = *", accessor2, ", true }")
		} else {
			p.P(idVar, ", ", okVar, " = ", accessor2, ", true")
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
	p.P("return &", wsDuplexName(reqRef, resRef), "{conn: conn}, nil")
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
	p.P("ctx := r.Context()")
	if correlated {
		idType := p.GoFieldType(idField.Type)
		p.P("out := &", wsOutName(s, m), "{conn: conn, pending: ", wsServerPendingConstructor, "[", idType, ", *", p.MessageTypeName(m.Request), "]()}")
		p.P("defer out.pending.closeAll()")
	} else {
		p.P("out := &wsConnOut[", resRef, "]{conn: conn}")
	}
	p.P("sendProtocolError := func(message string) {")
	p.P("_ = out.Send(ctx, &", resRef, "{})")
	p.P("_ = conn.Close(websocket.StatusInvalidFramePayloadData, message)")
	p.P("}")
	p.P("for {")
	p.P("_, data, err := conn.Read(ctx)")
	p.P("if err != nil { return }")
	p.P("frame := new(", p.MessageTypeName(m.Request), ")")
	p.P("if err := json.Unmarshal(data, frame); err != nil {")
	p.P(`sendProtocolError("invalid JSON frame")`)
	p.P("return")
	p.P("}")
	p.P("if validator, ok := any(frame).(interface{ Validate() error }); ok { if verr := validator.Validate(); verr != nil {")
	p.P(`sendProtocolError(verr.Error())`)
	p.P("return")
	p.P("} }")
	if correlated {
		writeWSIDExtraction(p, "frame", m.Request, idField, "replyID", "replyIDOk")
		p.P("if replyIDOk && out.pending.resolve(replyID, frame) { continue }")
	}
	p.P("if err := srv.", PascalCase(m.Name), "(ctx, frame, out); err != nil {")
	p.P("writeHandlerError(w, err)")
	p.P("return")
	p.P("}")
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", "GET"), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), "}))")
}

var _ = strings.ToUpper // reserved for future verb normalization in WS metadata
