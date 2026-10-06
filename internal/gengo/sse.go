package gengo

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

//go:embed runtime/sse_server.go.tmpl
var sseServerRuntimeSource string

//go:embed runtime/sse_client.go.tmpl
var sseClientRuntimeSource string

// writeSSEServerRuntime emits the shared SSESender type used by every
// streaming method's server interface. Errors returned before the first
// Send() still get a normal HTTP error response (headers aren't committed
// yet); errors after Send() has been called can only surface as an SSE
// "error" event, since the 200 response is already on the wire.
func writeSSEServerRuntime(p *Printer) {
	p.P(sseServerRuntimeSource)
}

func writeSSERoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path

	p.P("mux.Handle(", fmt.Sprintf("%q", strings.ToUpper(verb)+" "+fullPath), ", o.wrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {")
	writePrincipalLookup(p, m)
	p.P("req := new(", p.MessageTypeName(m.Request), ")")

	bodyBearing := onkir.IsBodyBearingVerb(verb)
	if bodyBearing {
		writeBodyBinding(p, m)
	}
	writePathParamBinding(p, path, m.Request)
	if !bodyBearing {
		writeQueryParamBinding(p, m.Request)
	}

	for _, h := range m.Service.Headers {
		writeHeaderCheck(p, h)
	}
	for _, h := range m.Headers {
		writeHeaderCheck(p, h)
	}

	writeValidateCall(p)
	writeAuthorizeCall(p, m)

	p.P("sender := newSSESender(w, ", sseEventNameFunc(p, m), ")")
	p.P("stopHeartbeat := sender.heartbeat(r.Context(), o.sseHeartbeat, o.sseHeartbeatSet)")
	p.P("err := srv.", PascalCase(m.Name), "(withHTTPRequest(r), req, sender)")
	p.P("stopHeartbeat()")
	p.P("if err != nil {")
	p.P("if !sender.Sent() {")
	writeErrorHandling(p, m)
	p.P("return")
	p.P("}")
	for i, errType := range m.ErrorTypes {
		p.P(fmt.Sprintf("var streamErr%d *", i), p.MessageTypeName(errType))
	}
	p.P("switch {")
	for i := range m.ErrorTypes {
		p.P(fmt.Sprintf("case errors.As(err, &streamErr%d):", i))
		p.P(fmt.Sprintf(`_ = sender.SendWithEvent("error", streamErr%d)`, i))
	}
	p.P("default:")
	p.P(`_ = sender.SendWithEvent("error", o.errorEventBody(r, err))`)
	p.P("}")
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", strings.ToUpper(verb)), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), ", Scopes: ", scopesLiteral(m), ", Meta: ", metaLiteral(m), "}))")
}

// writeEventStreamRuntime emits the shared generic EventStream[T] client type
// used by every streaming method. It reads one JSON-encoded event per "data:"
// line using bufio.Reader (not Scanner, which caps lines at 64KiB). A
// preceding "event: error" line (see writeSSEServerRuntime) is surfaced
// through Err() instead of being decoded into T, mirroring the server's
// smart error handling on the client side.
func writeEventStreamRuntime(p *Printer) {
	p.P(sseClientRuntimeSource)
}

func writeSSEClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path

	writeDoc(p, deprecatedDoc(m.Doc, m.Deprecated))
	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name),
		"(ctx context.Context, req *", p.MessageTypeName(m.Request), ", opts ...CallOption) (*EventStream[",
		p.MessageTypeName(m.Response), "], error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return nil, fmt.Errorf("validate request: %w", err) } }`)

	p.P("path := ", fmt.Sprintf("%q", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		if onkir.IsWildcardParam(path, paramName) {
			p.P("for _, segment := range strings.Split(fmt.Sprint(req.", PascalCase(paramName), "), \"/\") {")
			p.P(`if segment == "." || segment == ".." { return nil, fmt.Errorf("invalid path parameter ` + paramName + `: dot segments are not allowed") }`)
			p.P("}")
		}
		p.P("path = strings.ReplaceAll(path, ", fmt.Sprintf("%q", onkir.PathPlaceholder(path, paramName)), ", ",
			goPathEscapeExpr(path, paramName, "req."+PascalCase(paramName)), ")")
	}
	bodyBearing := onkir.IsBodyBearingVerb(verb)
	writeClientBodyOrQuery(p, m, bodyBearing)

	p.P("httpReq, err := http.NewRequestWithContext(ctx, ",
		fmt.Sprintf("%q", strings.ToUpper(verb)), ", c.BaseURL+path, ", bodyReaderExpr(bodyBearing), ")")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("build request: %w", err)`)
	p.P("}")
	if bodyBearing {
		p.P(`httpReq.Header.Set("Content-Type", "application/json")`)
	}
	p.P(`httpReq.Header.Set("Accept", "text/event-stream")`)
	p.P("for k, v := range c.Headers {")
	p.P("httpReq.Header.Set(k, v)")
	p.P("}")
	p.P("applyCallOptions(httpReq, opts)")

	p.P("resp, err := c.HTTPClient.Do(httpReq)")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("do request: %w", err)`)
	p.P("}")

	p.P("if resp.StatusCode < 200 || resp.StatusCode >= 300 {")
	p.P("defer resp.Body.Close()")
	p.P("respBody, bodyErr := readResponseBody(resp.Body, c.MaxResponseBodyBytes)")
	p.P("if bodyErr != nil { return nil, bodyErr }")
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
	p.P(`return nil, &UnexpectedStatusError{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}`)
	p.P("}")

	p.P("return newEventStream[", p.MessageTypeName(m.Response), "](resp.Body, c.MaxSSELineBytes), nil")
	p.P("}")
	p.P()
}

func sseEventNameFunc(p *Printer, m *onkir.Method) string {
	field := m.StreamEventOneof()
	if field == nil {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("func(event any) string {\n")
	b.WriteString("msg, ok := event.(*" + p.MessageTypeName(m.Response) + ")\n")
	b.WriteString("if !ok || msg == nil { return \"\" }\n")
	b.WriteString("switch msg." + GoFieldName(field) + ".(type) {\n")
	for _, variant := range field.Oneof.Variants {
		b.WriteString("case *" + OneofVariantTypeName(m.Response, field, variant) + ":\nreturn " + fmt.Sprintf("%q", variant.Tag()) + "\n")
	}
	b.WriteString("}\nreturn \"\"\n}")
	return b.String()
}
