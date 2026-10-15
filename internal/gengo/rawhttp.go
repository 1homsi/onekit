package gengo

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func writeRawRoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path
	pattern, httpMethod := strings.ToUpper(verb)+" "+fullPath, strings.ToUpper(verb)
	if verb == onkir.VerbAny {
		pattern, httpMethod = fullPath, "ANY"
	}

	p.P("mux.Handle(", fmt.Sprintf("%q", pattern), ", o.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {")
	if names := onkir.PathParamNames(path); hasWildcardParam(path, names) {
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		p.P("RestorePreservedSegments(r, ", strings.Join(quoted, ", "), ")")
	}
	writePrincipalLookup(p, m)
	p.P("req := new(", p.MessageTypeName(m.Request), ")")
	if onkir.IsBodyBearingVerb(verb) {
		if limit, ok := m.MaxBodyBytes(); ok {
			p.P("if r.Body != nil { r.Body = http.MaxBytesReader(w, r.Body, ", limit, ") }")
		} else {
			p.P("if r.Body != nil { r.Body = http.MaxBytesReader(w, r.Body, requestBodyLimit(o.MaxRequestBodyBytes)) }")
		}
	}
	writePathParamBinding(p, path, m.Request)
	writeQueryParamBinding(p, m.Request)
	for _, h := range m.Service.Headers {
		writeHeaderCheck(p, h)
	}
	for _, h := range m.Headers {
		writeHeaderCheck(p, h)
	}
	writeValidateCall(p)
	writeAuthorizeCall(p, m)

	p.P("rw := &routeResponseWriter{ResponseWriter: w}")
	p.P("routeCtx, _ := RouteContext(rw, r)")
	p.P("if err := srv.", PascalCase(m.Name), "(rw, r.WithContext(routeCtx), req); err != nil {")
	p.P("if rw.wrote {")
	p.P("return")
	p.P("}")
	writeErrorHandling(p, m)
	p.P("}")
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", httpMethod), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), ", Scopes: ", scopesLiteral(m), ", Meta: ", metaLiteral(m), ", Guards: ", guardsLiteral(m), "}))")
}

func hasWildcardParam(path string, names []string) bool {
	for _, name := range names {
		if onkir.IsWildcardParam(path, name) {
			return true
		}
	}
	return false
}

func fileHasRawHTTP(file *onkir.File) bool {
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if m.IsRawHTTP() {
				return true
			}
		}
	}
	return false
}

func writeRouteResponseWriter(p *Printer) {
	p.P(`// routeResponseWriter records whether a hand-written @http handler has started`)
	p.P(`// the response, so an error it returns afterwards is not written on top of it.`)
	p.P(`type routeResponseWriter struct {`)
	p.P(`http.ResponseWriter`)
	p.P(`wrote bool`)
	p.P(`}`)
	p.P()
	p.P(`func (w *routeResponseWriter) WriteHeader(status int) {`)
	p.P(`w.wrote = true`)
	p.P(`w.ResponseWriter.WriteHeader(status)`)
	p.P(`}`)
	p.P()
	p.P(`func (w *routeResponseWriter) Write(data []byte) (int, error) {`)
	p.P(`w.wrote = true`)
	p.P(`return w.ResponseWriter.Write(data)`)
	p.P(`}`)
	p.P()
	p.P(`func (w *routeResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }`)
	p.P()
	p.P(`func (w *routeResponseWriter) Flush() {`)
	p.P(`if flusher, ok := w.ResponseWriter.(http.Flusher); ok {`)
	p.P(`w.wrote = true`)
	p.P(`flusher.Flush()`)
	p.P(`}`)
	p.P(`}`)
	p.P()
	p.P(`func (w *routeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {`)
	p.P(`hijacker, ok := w.ResponseWriter.(http.Hijacker)`)
	p.P(`if !ok {`)
	p.P(`return nil, nil, http.ErrNotSupported`)
	p.P(`}`)
	p.P(`conn, rw, err := hijacker.Hijack()`)
	p.P(`if err == nil {`)
	p.P(`w.wrote = true`)
	p.P(`}`)
	p.P(`return conn, rw, err`)
	p.P(`}`)
	p.P()
}

func writeRawServiceMethod(p *Printer, m *onkir.Method) {
	p.P(PascalCase(m.Name), "(w http.ResponseWriter, r *http.Request, req *", p.MessageTypeName(m.Request), ") error")
}

func writeRawURLMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.Path()
	fullPath := s.BasePath + path
	writeDoc(p, deprecatedDoc(m.Doc, m.Deprecated))
	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name), "URL(req *", p.MessageTypeName(m.Request), ") (string, error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return "", fmt.Errorf("validate request: %w", err) } }`)
	writeClientPathBuildReturning(p, m, path, fullPath, `""`)
	writeClientQueryParams(p, m.Request)
	p.P("return c.BaseURL + path, nil")
	p.P("}")
	p.P()
}

func writeRawClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	writeRawURLMethod(p, s, m)
	if !m.RawIsFetchable() {
		return
	}
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path
	bodyBearing := onkir.IsBodyBearingVerb(verb)
	requestKind := m.RawRequestKind()
	typedResponse := m.RawResponseKind() == onkir.RawBodyJSON

	writeDoc(p, deprecatedDoc(m.Doc, m.Deprecated))
	signature := "(ctx context.Context, req *" + p.MessageTypeName(m.Request)
	if verb == onkir.VerbAny {
		signature += ", method string"
	}
	if bodyBearing && requestKind != onkir.RawBodyJSON && requestKind != onkir.RawBodyNone {
		signature += ", body io.Reader, contentType string"
	}
	result := "*http.Response"
	if typedResponse {
		result = "*" + p.MessageTypeName(m.Response)
	}
	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name), signature, ", opts ...CallOption) (", result, ", error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return nil, fmt.Errorf("validate request: %w", err) } }`)
	writeClientPathBuild(p, m, path, fullPath)
	writeClientQueryParams(p, m.Request)
	bodyArg := "nil"
	switch {
	case requestKind == onkir.RawBodyJSON:
		field := rawBodyField(m)
		p.P("payload, err := marshalRequestJSON(req.", GoFieldName(field), ")")
		p.P("if err != nil {")
		p.P(`return nil, fmt.Errorf("marshal request: %w", err)`)
		p.P("}")
		bodyArg = "bytes.NewReader(payload)"
	case bodyBearing && requestKind != onkir.RawBodyNone:
		bodyArg = "body"
	}
	methodExpr := fmt.Sprintf("%q", strings.ToUpper(verb))
	if verb == onkir.VerbAny {
		methodExpr = "method"
		p.P(`if method == "" { method = http.MethodGet }`)
	}
	p.P("httpReq, err := http.NewRequestWithContext(ctx, ", methodExpr, ", c.BaseURL+path, ", bodyArg, ")")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("build request: %w", err)`)
	p.P("}")
	switch {
	case requestKind == onkir.RawBodyJSON:
		p.P(`httpReq.Header.Set("Content-Type", "application/json")`)
	case bodyBearing && requestKind != onkir.RawBodyNone:
		p.P(`if contentType != "" { httpReq.Header.Set("Content-Type", contentType) }`)
	}
	p.P("for k, v := range c.Headers {")
	p.P("httpReq.Header.Set(k, v)")
	p.P("}")
	p.P("applyCallOptions(httpReq, opts)")
	p.P("resp, err := c.HTTPClient.Do(httpReq)")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("do request: %w", err)`)
	p.P("}")
	if typedResponse {
		p.P("defer resp.Body.Close()")
		writeClientErrorHandling(p, m)
		p.P("result := new(", p.MessageTypeName(m.Response), ")")
		p.P("data, err := readResponseBody(resp.Body, c.MaxResponseBodyBytes)")
		p.P("if err != nil {")
		p.P(`return nil, fmt.Errorf("decode response: %w", err)`)
		p.P("}")
		p.P("if err := unmarshalJSONValue(data, result); err != nil && !jsonBlank(data) {")
		p.P(`return nil, fmt.Errorf("decode response: %w", err)`)
		p.P("}")
		p.P("return result, nil")
	} else {
		p.P("return resp, nil")
	}
	p.P("}")
	p.P()
}

func rawBodyField(m *onkir.Method) *onkir.Field {
	name, _ := m.BodyField()
	return onkir.FindField(m.Request, name)
}

// withMethodServices returns file without the services that declare no RPCs:
// a server or client generated for one would not compile.
func withMethodServices(file *onkir.File) *onkir.File {
	empty := false
	for _, s := range file.Services {
		empty = empty || len(s.Methods) == 0
	}
	if !empty {
		return file
	}
	filtered := *file
	filtered.Services = nil
	for _, s := range file.Services {
		if len(s.Methods) > 0 {
			filtered.Services = append(filtered.Services, s)
		}
	}
	return &filtered
}
