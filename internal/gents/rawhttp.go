package gents

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func rawURLFuncName(s *onkir.Service, m *onkir.Method) string {
	return CamelCase(s.Name) + PascalCase(m.Name) + "Url"
}

func rawRequestOptions(m *onkir.Method) string {
	verb, _ := m.Verb()
	options := "RequestOptions"
	extra := ""
	if verb == onkir.VerbAny {
		extra = "method?: string; "
	}
	switch m.RawRequestKind() {
	case onkir.RawBodyStream:
		extra += "body?: BodyInit | null; contentType?: string; "
	case onkir.RawBodyMultipart:
		extra += "body: FormData; "
	}
	if extra == "" {
		return options
	}
	return options + " & { " + strings.TrimSuffix(extra, " ") + " }"
}

func writeRawClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	typedResponse := m.RawResponseKind() == onkir.RawBodyJSON
	fn := rawURLFuncName(s, m)
	reqType := p.MessageTypeName(m.Request)

	writeJSDoc(p, tsDeprecatedDoc(m.Doc, m.Deprecated))
	p.P(CamelCase(m.Name), "Url(req: ", reqType, "): string {")
	p.P("return ", fn, "(this.baseUrl, req, this.options.validateRequests !== false);")
	p.P("}")
	p.P()
	if !m.RawIsFetchable() {
		return
	}

	writeJSDoc(p, tsDeprecatedDoc(m.Doc, m.Deprecated))
	result := "Response"
	if typedResponse {
		result = p.MessageTypeName(m.Response)
	}
	optional := "?"
	if m.RawRequestKind() == onkir.RawBodyMultipart {
		optional = ""
	}
	p.P("async ", CamelCase(m.Name), "(req: ", reqType, ", opts", optional, ": ", rawRequestOptions(m), "): Promise<", result, "> {")
	methodExpr := fmt.Sprintf("%q", strings.ToUpper(verb))
	if verb == onkir.VerbAny {
		methodExpr = `opts?.method ?? "GET"`
	}
	assign := "return"
	if typedResponse {
		assign = "const res ="
	}
	p.P(assign, " await this.request(", fn, "(this.baseUrl, req, this.options.validateRequests !== false), {")
	p.P("method: ", methodExpr, ",")
	switch m.RawRequestKind() {
	case onkir.RawBodyJSON:
		field, _ := m.BodyField()
		p.P(`headers: { "Content-Type": "application/json", ...opts?.headers },`)
		p.P("body: JSON.stringify(", p.MessageCodecName(m.Request, "encode"), "(req)[", fmt.Sprintf("%q", field), "]),")
	case onkir.RawBodyStream:
		p.P(`headers: { ...(opts?.contentType ? { "Content-Type": opts.contentType } : {}), ...opts?.headers },`)
		p.P("body: opts?.body ?? null,")
	case onkir.RawBodyMultipart:
		p.P("headers: { ...opts?.headers },")
		p.P("body: opts.body,")
	default:
		p.P("headers: { ...opts?.headers },")
	}
	p.P("signal: requestSignal(opts?.signal, opts?.timeoutMs ?? this.options.timeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS),")
	p.P("});")
	if typedResponse {
		p.P()
		p.P("if (!res.ok) {")
		writeClientErrorHandling(p, m)
		p.P("}")
		p.P()
		p.P("const text = await readResponseText(res, this.options.maxResponseBodyBytes);")
		p.P("return ", p.MessageCodecName(m.Response, "decode"), `(text.trim() === "" ? {} : JSON.parse(text));`)
	}
	p.P("}")
	p.P()
}

func writeRawURLFunc(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.Path()
	fullPath := s.BasePath + path
	writeJSDoc(p, tsDeprecatedDoc(m.Doc, m.Deprecated))
	p.P("export function ", rawURLFuncName(s, m), "(baseUrl: string, req: ", p.MessageTypeName(m.Request), ", validate = true): string {")
	writeClientRequestValidation(p, m, "validate")
	p.P(fmt.Sprintf("let path = %q;", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		if onkir.IsWildcardParam(path, paramName) || isStringField(field) {
			p.P(fmt.Sprintf(`if (String(req.%s).split("/").some((segment) => segment === "." || segment === "..")) throw new RequestValidationError("invalid request", [%q]);`,
				p.naming.ident(field.Name), paramName+": dot segments are not allowed"))
		}
		p.P(fmt.Sprintf(
			"path = path.replace(%q, %s);",
			onkir.PathPlaceholder(path, paramName), tsPathEncodeExpr(path, paramName, "req."+p.naming.ident(field.Name)),
		))
	}
	writeClientQueryParams(p, m.Request)
	p.P("return baseUrl + path;")
	p.P("}")
	p.P()
}

func writeRawURLFuncs(p *Printer, s *onkir.Service) {
	for _, m := range s.Methods {
		if m.IsRawHTTP() {
			writeRawURLFunc(p, s, m)
		}
	}
}

func serviceNeedsRequestHelper(s *onkir.Service) bool {
	for _, m := range s.Methods {
		if !m.IsWebSocket() && (!m.IsRawHTTP() || m.RawIsFetchable()) {
			return true
		}
	}
	return false
}
