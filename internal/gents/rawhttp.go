package gents

import (
	"fmt"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func writeRawClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path
	bodyBearing := onkir.IsBodyBearingVerb(verb)

	writeJSDoc(p, tsDeprecatedDoc(m.Doc, m.Deprecated))
	options := "RequestOptions"
	if bodyBearing {
		options = "RequestOptions & { body?: BodyInit | null; contentType?: string }"
	}
	p.P("async ", CamelCase(m.Name), "(req: ", p.MessageTypeName(m.Request), ", opts?: ", options, "): Promise<Response> {")
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(req);")
	p.P(`if (violations.length > 0) throw new RequestValidationError("invalid request", violations);`)
	p.P(fmt.Sprintf("let path = %q;", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		if onkir.IsWildcardParam(path, paramName) {
			p.P(fmt.Sprintf(`if (String(req.%s).split("/").some((segment) => segment === "." || segment === "..")) throw new RequestValidationError("invalid request", [%q]);`,
				p.naming.ident(field.Name), paramName+": dot segments are not allowed"))
		}
		p.P(fmt.Sprintf(
			"path = path.replace(%q, %s);",
			onkir.PathPlaceholder(path, paramName), tsPathEncodeExpr(path, paramName, "req."+p.naming.ident(field.Name)),
		))
	}
	writeClientQueryParams(p, m.Request)
	p.P("return this.request(this.baseUrl + path, {")
	p.P(fmt.Sprintf("method: %q,", strings.ToUpper(verb)))
	if bodyBearing {
		p.P(`headers: { ...(opts?.contentType ? { "Content-Type": opts.contentType } : {}), ...opts?.headers },`)
		p.P("body: opts?.body ?? null,")
	} else {
		p.P("headers: { ...opts?.headers },")
	}
	p.P("signal: requestSignal(opts?.signal, opts?.timeoutMs ?? this.options.timeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS),")
	p.P("});")
	p.P("}")
	p.P()
}
