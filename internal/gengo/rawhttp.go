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

	p.P("mux.Handle(", fmt.Sprintf("%q", strings.ToUpper(verb)+" "+fullPath), ", o.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {")
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
	p.P("}), RequestMetadata{Service: ", fmt.Sprintf("%q", s.Name), ", Method: ", fmt.Sprintf("%q", m.Name), ", HTTPMethod: ", fmt.Sprintf("%q", strings.ToUpper(verb)), ", Route: ", fmt.Sprintf("%q", fullPath), ", AuthSchemes: ", authSchemesLiteral(s, m), ", Scopes: ", scopesLiteral(m), ", Meta: ", metaLiteral(m), ", Guards: ", guardsLiteral(m), "}))")
}

func writeRawServiceMethod(p *Printer, m *onkir.Method) {
	p.P(PascalCase(m.Name), "(w http.ResponseWriter, r *http.Request, req *", p.MessageTypeName(m.Request), ") error")
}

func writeRawClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path
	bodyBearing := onkir.IsBodyBearingVerb(verb)

	writeDoc(p, deprecatedDoc(m.Doc, m.Deprecated))
	signature := "(ctx context.Context, req *" + p.MessageTypeName(m.Request)
	if bodyBearing {
		signature += ", body io.Reader, contentType string"
	}
	p.P("func (c *", s.Name, "Client) ", PascalCase(m.Name), signature, ", opts ...CallOption) (*http.Response, error) {")
	p.P(`if validator, ok := any(req).(interface{ Validate() error }); ok { if err := validator.Validate(); err != nil { return nil, fmt.Errorf("validate request: %w", err) } }`)
	writeClientPathBuild(p, m, path, fullPath)
	writeClientQueryParams(p, m.Request)
	bodyArg := "nil"
	if bodyBearing {
		bodyArg = "body"
	}
	p.P("httpReq, err := http.NewRequestWithContext(ctx, ", fmt.Sprintf("%q", strings.ToUpper(verb)), ", c.BaseURL+path, ", bodyArg, ")")
	p.P("if err != nil {")
	p.P(`return nil, fmt.Errorf("build request: %w", err)`)
	p.P("}")
	if bodyBearing {
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
	p.P("return resp, nil")
	p.P("}")
	p.P()
}
