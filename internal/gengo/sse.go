package gengo

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
)

func fileHasStreamMethods(file *onkir.File) bool {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsStream() {
				return true
			}
		}
	}
	return false
}

// writeSSEServerRuntime emits the shared SSESender type used by every
// streaming method's server interface. Errors returned before the first
// Send() still get a normal HTTP error response (headers aren't committed
// yet); errors after Send() has been called can only surface as an SSE
// "error" event, since the 200 response is already on the wire.
func writeSSESenderType(p *Printer) {
	p.P("type SSESender interface {")
	p.P("Send(event any) error")
	p.P("SendWithEvent(eventType string, event any) error")
	p.P("Flush()")
	p.P("}")
	p.P()

	p.P("type sseSender struct {")
	p.P("w http.ResponseWriter")
	p.P("flusher http.Flusher")
	p.P("started bool")
	p.P("}")
	p.P()

	p.P("func newSSESender(w http.ResponseWriter) *sseSender {")
	p.P("return &sseSender{w: w}")
	p.P("}")
	p.P()

	p.P("func (s *sseSender) Sent() bool { return s.started }")
	p.P()

	p.P("func (s *sseSender) Send(event any) error {")
	p.P(`return s.SendWithEvent("", event)`)
	p.P("}")
	p.P()
}

func writeSSESenderStart(p *Printer) {
	p.P("func (s *sseSender) start() {")
	p.P("s.started = true")
	p.P(`s.w.Header().Set("Content-Type", "text/event-stream")`)
	p.P(`s.w.Header().Set("Cache-Control", "no-cache")`)
	p.P(`s.w.Header().Set("Connection", "keep-alive")`)
	p.P("s.w.WriteHeader(http.StatusOK)")
	p.P("if f, ok := s.w.(http.Flusher); ok {")
	p.P("s.flusher = f")
	p.P("}")
	p.P("}")
	p.P()
}

func writeSSESenderSendWithEvent(p *Printer) {
	p.P("func (s *sseSender) SendWithEvent(eventType string, event any) error {")
	p.P("data, err := json.Marshal(event)")
	p.P("if err != nil {")
	p.P("return err")
	p.P("}")
	p.P("if !s.started {")
	p.P("s.start()")
	p.P("}")
	p.P("if eventType != \"\" {")
	p.P(`fmt.Fprintf(s.w, "event: %s\n", eventType)`)
	p.P("}")
	p.P(`fmt.Fprintf(s.w, "data: %s\n\n", data)`)
	p.P("s.Flush()")
	p.P("return nil")
	p.P("}")
	p.P()

	p.P("func (s *sseSender) Flush() {")
	p.P("if s.flusher != nil {")
	p.P("s.flusher.Flush()")
	p.P("}")
	p.P("}")
	p.P()
}

// writeSSEServerRuntime emits the shared SSESender type used by every
// streaming method's server interface. Errors returned before the first
// Send() still get a normal HTTP error response (headers aren't committed
// yet); errors after Send() has been called can only surface as an SSE
// "error" event, since the 200 response is already on the wire.
func writeSSEServerRuntime(p *Printer) {
	writeSSESenderType(p)
	writeSSESenderStart(p)
	writeSSESenderSendWithEvent(p)
}

func writeSSERoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path

	p.P(`mux.Handle("`, upperVerb(verb), " ", fullPath, `", o.wrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {`)
	p.P("req := new(", p.MessageTypeName(m.Request), ")")

	writePathParamBinding(p, path, m.Request)
	writeQueryParamBinding(p, m.Request)

	for _, h := range m.Service.Headers {
		writeHeaderCheck(p, h)
	}
	for _, h := range m.Headers {
		writeHeaderCheck(p, h)
	}

	writeValidateCall(p)

	p.P("sender := newSSESender(w)")
	p.P("if err := srv.", PascalCase(m.Name), "(r.Context(), req, sender); err != nil {")
	p.P("if !sender.Sent() {")
	writeErrorHandling(p, m)
	p.P("return")
	p.P("}")
	p.P("switch e := err.(type) {")
	for _, errType := range m.ErrorTypes {
		p.P("case *", p.MessageTypeName(errType), ":")
		p.P(`_ = sender.SendWithEvent("error", e)`)
	}
	p.P("default:")
	p.P(`_ = sender.SendWithEvent("error", map[string]string{"message": err.Error()})`)
	p.P("}")
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", upperVerb(verb)), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), "}))")
}

func upperVerb(verb string) string {
	switch verb {
	case "get":
		return "GET"
	case "post":
		return "POST"
	case "put":
		return "PUT"
	case "patch":
		return "PATCH"
	case "delete":
		return "DELETE"
	case "query":
		return "QUERY"
	default:
		return verb
	}
}

// writeEventStreamRuntime emits the shared generic EventStream[T] client type
// used by every streaming method. It reads one JSON-encoded event per "data:"
// line using bufio.Reader (not Scanner, which caps lines at 64KiB). A
// preceding "event: error" line (see writeSSEServerRuntime) is surfaced
// through Err() instead of being decoded into T, mirroring the server's
// smart error handling on the client side.
func writeEventStreamRuntime(p *Printer) {
	p.P("type EventStream[T any] struct {")
	p.P("body io.ReadCloser")
	p.P("reader *bufio.Reader")
	p.P("err error")
	p.P("pendingEvent string")
	p.P("}")
	p.P()

	p.P("func newEventStream[T any](body io.ReadCloser) *EventStream[T] {")
	p.P("return &EventStream[T]{body: body, reader: bufio.NewReader(body)}")
	p.P("}")
	p.P()

	p.P("func (s *EventStream[T]) Next(event *T) bool {")
	p.P("for {")
	p.P("line, err := s.reader.ReadString('\\n')")
	p.P("if err != nil {")
	p.P("if err != io.EOF {")
	p.P("s.err = err")
	p.P("}")
	p.P("return false")
	p.P("}")
	p.P(`line = strings.TrimRight(line, "\r\n")`)
	p.P(`if strings.HasPrefix(line, "event:") {`)
	p.P(`s.pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))`)
	p.P("continue")
	p.P("}")
	p.P(`if !strings.HasPrefix(line, "data:") {`)
	p.P("continue")
	p.P("}")
	p.P(`data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))`)
	p.P(`eventType := s.pendingEvent`)
	p.P(`s.pendingEvent = ""`)
	p.P(`if eventType == "error" {`)
	p.P(`s.err = fmt.Errorf("stream error: %s", data)`)
	p.P("return false")
	p.P("}")
	p.P("if err := json.Unmarshal([]byte(data), event); err != nil {")
	p.P("s.err = err")
	p.P("return false")
	p.P("}")
	p.P("return true")
	p.P("}")
	p.P("}")
	p.P()

	p.P("func (s *EventStream[T]) Err() error { return s.err }")
	p.P("func (s *EventStream[T]) Close() error { return s.body.Close() }")
	p.P()
}

func writeSSEClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path

	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name),
		"(ctx context.Context, req *", p.MessageTypeName(m.Request), ") (*EventStream[",
		p.MessageTypeName(m.Response), "], error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return nil, fmt.Errorf("validate request: %w", err) } }`)

	p.P("path := ", fmt.Sprintf("%q", fullPath))
	for _, paramName := range pathParamNames(path) {
		field := findField(m.Request, paramName)
		if field == nil {
			continue
		}
		p.P("path = strings.ReplaceAll(path, ", fmt.Sprintf("%q", "{"+paramName+"}"), ", ",
			fmt.Sprintf("fmt.Sprintf(%q, req.%s)", "%v", PascalCase(paramName)), ")")
	}
	writeClientQueryParams(p, m.Request)

	p.P("httpReq, err := http.NewRequestWithContext(ctx, ",
		fmt.Sprintf("%q", upperVerb(verb)), ", c.BaseURL+path, nil)")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("build request: %w", err)`)
	p.P("}")
	p.P(`httpReq.Header.Set("Accept", "text/event-stream")`)
	p.P("for k, v := range c.Headers {")
	p.P("httpReq.Header.Set(k, v)")
	p.P("}")

	p.P("resp, err := c.HTTPClient.Do(httpReq)")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("do request: %w", err)`)
	p.P("}")

	p.P("if resp.StatusCode < 200 || resp.StatusCode >= 300 {")
	p.P("defer resp.Body.Close()")
	p.P("respBody, _ := io.ReadAll(resp.Body)")
	for _, errType := range m.ErrorTypes {
		status := 500
		if code, ok := errType.StatusCode(); ok {
			status = code
		}
		p.P(fmt.Sprintf("if resp.StatusCode == %d {", status))
		p.P("e := new(", p.MessageTypeName(errType), ")")
		p.P("if jsonErr := json.Unmarshal(respBody, e); jsonErr == nil {")
		p.P("return nil, e")
		p.P("}")
		p.P("}")
	}
	p.P(`return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))`)
	p.P("}")

	p.P("return newEventStream[", p.MessageTypeName(m.Response), "](resp.Body), nil")
	p.P("}")
	p.P()
}
