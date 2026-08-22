package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

// --- shared runtime -------------------------------------------------------

// writeWSOutType emits the server-side send interface for bidirectional
// WebSocket methods. Handlers push response frames through it while the
// generated read loop owns the connection.
func writeWSOutType(p *Printer) {
	p.P("// WSOut sends one direction of a bidirectional WebSocket RPC: server-")
	p.P("// to-client frames of the method's declared response type.")
	p.P("type WSOut[E any] interface {")
	p.P("Send(ctx context.Context, value *E) error")
	p.P("}")
	p.P()
	p.P("type wsConnOut[E any] struct { conn *websocket.Conn }")
	p.P()
	p.P("func (s *wsConnOut[E]) Send(ctx context.Context, value *E) error {")
	p.P("data, err := json.Marshal(value)")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("return s.conn.Write(ctx, websocket.MessageText, data)")
	p.P("}")
	p.P()
}

// writeWSDuplexType emits the client-side duplex handle returned by
// WebSocket client methods: explicit Send/Receive/Close with no hidden
// goroutines.
func writeWSDuplexType(p *Printer, inName, outName string) {
	p.P("// ", wsDuplexName(inName, outName), " is a bidirectional WebSocket connection:")
	// client-to-server frames of the request type flow through Send;
	// server-to-client frames surface from Receive.
	p.P("type ", wsDuplexName(inName, outName), " struct { conn *websocket.Conn }")
	p.P()
	p.P("func (d *", wsDuplexName(inName, outName), ") Send(ctx context.Context, value *", inName, ") error {")
	p.P("if validator, ok := any(value).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return fmt.Errorf(\"validate frame: %w\", err) } }")
	p.P("data, err := json.Marshal(value)")
	p.P("if err != nil { return fmt.Errorf(\"marshal frame: %w\", err) }")
	p.P("return d.conn.Write(ctx, websocket.MessageText, data)")
	p.P("}")
	p.P()
	p.P("func (d *", wsDuplexName(inName, outName), ") Receive(ctx context.Context) (*", outName, ", error) {")
	p.P("_, data, err := d.conn.Read(ctx)")
	p.P("if err != nil { return nil, err }")
	p.P("frame := new(", outName, ")")
	p.P("if err := json.Unmarshal(data, frame); err != nil { return nil, fmt.Errorf(\"decode frame: %w\", err) }")
	p.P("return frame, nil")
	p.P("}")
	p.P()
	p.P("func (d *", wsDuplexName(inName, outName), ") Close() error { return d.conn.Close(websocket.StatusNormalClosure, \"\") }")
	p.P()
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
	p.P(`socketURL := path`)
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
	p.P("out := &wsConnOut[", resRef, "]{conn: conn}")
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
	p.P("if err := srv.", PascalCase(m.Name), "(ctx, frame, out); err != nil {")
	p.P("writeHandlerError(w, err)")
	p.P("return")
	p.P("}")
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", "GET"), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), "}))")
}

var _ = strings.ToUpper // reserved for future verb normalization in WS metadata
