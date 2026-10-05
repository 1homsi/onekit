package gents

import (
	"fmt"
	"slices"
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

// writeSSEResponseHelper turns a handler's ReadableStream<T> into an SSE HTTP
// response. It reads the first chunk before building the Response so an error
// thrown before any event is produced still gets a normal status-coded JSON
// error (via errorResponse) instead of a text/event-stream response - headers
// aren't committed until the first successful read. An error thrown after at
// least one event was read can no longer change the response, so it's
// emitted as an "event: error" SSE frame instead.
// sseResponse takes an encode callback (rather than trying to dispatch on T
// generically) so each streamed value crosses the wire the same way a
// non-streamed response does - camelCase TS shape in, snake_case JSON shape
// out - matching the per-message encode<Response> function (see types.go).
func writeSSEResponseHelper(p *Printer) {
	p.P("const defaultSSEHeartbeatMs = 15000;")
	p.P()
	p.P("async function sseResponse<T>(req: Request, stream: ReadableStream<T>, encode: (v: T) => unknown, eventName?: (encoded: any) => string | undefined): Promise<Response> {")
	p.P("const reader = stream.getReader();")
	p.P("const interval = sseHeartbeats.get(req) ?? defaultSSEHeartbeatMs;")
	p.P("const firstRead = reader.read();")
	p.P("let first: ReadableStreamReadResult<T> | undefined;")
	p.P("try {")
	p.P("if (interval > 0) {")
	p.P("let timer: ReturnType<typeof setTimeout> | undefined;")
	p.P(`const timeout = new Promise<"timeout">((resolve) => { timer = setTimeout(() => resolve("timeout"), interval); });`)
	p.P("const raced = await Promise.race([firstRead, timeout]);")
	p.P("clearTimeout(timer);")
	p.P(`if (raced !== "timeout") first = raced;`)
	p.P("} else {")
	p.P("first = await firstRead;")
	p.P("}")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")
	p.P()
	p.P("const encoder = new TextEncoder();")
	p.P("let ping: ReturnType<typeof setInterval> | undefined;")
	p.P("const body = new ReadableStream<Uint8Array>({")
	p.P("async start(controller) {")
	p.P(`const sendPing = () => { try { controller.enqueue(encoder.encode(": ping\n\n")); } catch { } };`)
	p.P("if (interval > 0) ping = setInterval(sendPing, interval);")
	p.P("if (first === undefined) sendPing();")
	p.P("try {")
	p.P("let current = first ?? (await firstRead);")
	p.P("while (!current.done) {")
	p.P("const encoded = encode(current.value);")
	p.P("const raw = eventName?.(encoded);")
	p.P("const name = typeof raw === \"string\" && /^[A-Za-z0-9_.-]+$/.test(raw) ? raw : undefined;")
	p.P(`controller.enqueue(encoder.encode((name ? "event: " + name + "\n" : "") + "data: " + JSON.stringify(encoded) + "\n\n"));`)
	p.P("current = await reader.read();")
	p.P("}")
	p.P("} catch (err) {")
	p.P(`const errBody = err instanceof HttpError ? err.body : { message: "internal server error" };`)
	p.P(`controller.enqueue(encoder.encode("event: error\ndata: " + JSON.stringify(errBody) + "\n\n"));`)
	p.P("} finally {")
	p.P("if (ping !== undefined) clearInterval(ping);")
	p.P("try { controller.close(); } catch { }")
	p.P("}")
	p.P("},")
	p.P("cancel() {")
	p.P("if (ping !== undefined) clearInterval(ping);")
	p.P("return reader.cancel();")
	p.P("},")
	p.P("});")
	p.P()
	p.P("return new Response(body, {")
	p.P("status: 200,")
	p.P(
		`headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", "Connection": "keep-alive" },`,
	)
	p.P("});")
	p.P("}")
	p.P()
}

func sseEventNameExpr(m *onkir.Method) string {
	field := m.StreamEventOneof()
	if field == nil {
		return ""
	}
	disc, _ := field.Oneof.Discriminator()
	return fmt.Sprintf("(encoded) => encoded?.[%q]?.[%q] ?? encoded?.[%q]", field.Name, disc, disc)
}

func writeSSEHandlerMethod(p *Printer, m *onkir.Method) {
	p.P(CamelCase(m.Name), "(req: ", p.MessageTypeName(m.Request),
		", context: RequestContext): ReadableStream<", p.MessageTypeName(m.Response), ">;")
}

func writeSSERoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	path, _ := m.Path()
	fullPath := s.BasePath + path
	hasPathParams := len(onkir.PathParamNames(path)) > 0

	p.P("{")
	p.P(fmt.Sprintf("method: %q,", strings.ToUpper(verb)))
	p.P(fmt.Sprintf("path: %q,", fullPath))
	writeRouteScopes(p, m)
	writeRouteMeta(p, m)
	writeRoutePrincipalFlag(p, m)
	p.P("handler: async (req: Request): Promise<Response> => {")

	p.P("const url = new URL(req.url);")
	if hasPathParams {
		p.P(fmt.Sprintf("const match = matchPath(%q, url.pathname);", fullPath))
		p.P(`if (!match) return new Response("Not Found", { status: 404 });`)
	}

	p.P("try {")
	for _, header := range slices.Concat(s.Headers, m.Headers) {
		format, hasFormat := header.Format()
		p.P("{")
		p.P("const value = req.headers.get(", fmt.Sprintf("%q", header.Name), ");")
		if header.Required() {
			p.P("if (!value) throw requestError(400, \"missing_header\", ", fmt.Sprintf("%q", "missing required header: "+header.Name), ", { field: ", fmt.Sprintf("%q", header.Name), " });")
		}
		if hasFormat {
			p.P("if (value && !validHeaderFormat(value, ", fmt.Sprintf("%q", format), ")) throw requestError(400, \"invalid_header\", ", fmt.Sprintf("%q", "invalid header "+header.Name+": expected "+format), ", { field: ", fmt.Sprintf("%q", header.Name), " });")
		}
		p.P("}")
	}
	bodyBearing := onkir.IsBodyBearingVerb(verb)
	p.P("let body: any = {};")
	if bodyBearing {
		if bodyField, ok := m.BodyField(); ok {
			p.P("body[", fmt.Sprintf("%q", bodyField), "] = await readJSONBody(req);")
		} else {
			p.P("body = (await readJSONBody(req)) ?? {};")
		}
	} else {
		writeServerQueryParams(p, m.Request)
	}
	if hasPathParams {
		for _, paramName := range onkir.PathParamNames(path) {
			field := onkir.FindField(m.Request, paramName)
			if field == nil {
				continue
			}
			if field.Type != nil && field.Type.Kind == onkir.KindScalar {
				p.P("body.", field.Name, " = parseScalar(match[", fmt.Sprintf("%q", paramName), "] ?? \"\", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = match[", fmt.Sprintf("%q", paramName), "] ?? \"\";")
			}
		}
	}

	p.P("const decoded = ", p.MessageCodecName(m.Request, "decode"), "(body);")
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(decoded);")
	p.P("if (violations.length > 0) throw requestError(400, \"validation_failed\", violations.join(\"; \"), { violations });")
	writeRouteAuthorizeCall(p, m)
	p.P("const stream = handler.", CamelCase(m.Name), "(decoded, ", routeContextLiteral(m), ");")
	eventName := sseEventNameExpr(m)
	if eventName != "" {
		eventName = ", " + eventName
	}
	p.P("return await sseResponse(req, stream, ", p.MessageCodecName(m.Response, "encode"), eventName, ");")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")

	p.P("},")
	p.P("},")
}

func writeSSEClientFetch(p *Printer, m *onkir.Method) {
	verb, _ := m.Verb()
	p.P("const res = await this.request(this.baseUrl + path, {")
	p.P(fmt.Sprintf("method: %q,", strings.ToUpper(verb)))
	if onkir.IsBodyBearingVerb(verb) {
		p.P(`headers: { "Content-Type": "application/json", Accept: "text/event-stream", ...this.options.defaultHeaders, ...opts?.headers },`)
		if bodyField, ok := m.BodyField(); ok {
			p.P("body: JSON.stringify(", p.MessageCodecName(m.Request, "encode"), "(req)[", fmt.Sprintf("%q", bodyField), "]),")
		} else {
			p.P("body: JSON.stringify(", p.MessageCodecName(m.Request, "encode"), "(req)),")
		}
	} else {
		p.P(`headers: { Accept: "text/event-stream", ...this.options.defaultHeaders, ...opts?.headers },`)
	}
	p.P("signal: opts?.signal ?? null,")
	p.P("});")
	p.P()

	p.P("if (!res.ok) {")
	writeClientErrorHandling(p, m)
	p.P("}")
	p.P()

	p.P("if (!res.body) {")
	p.P("return;")
	p.P("}")
}

func writeSSEClientReadLoop(p *Printer, m *onkir.Method) {
	p.P("const reader = res.body.getReader();")
	p.P("const decoder = new TextDecoder();")
	p.P("const maxSSELineBytes = this.options.maxSSELineBytes && this.options.maxSSELineBytes > 0 ? this.options.maxSSELineBytes : DEFAULT_MAX_SSE_LINE_BYTES;")
	p.P("const tooLong = (text: string) => new TextEncoder().encode(text).byteLength > maxSSELineBytes;")
	p.P(`let buffer = "";`)
	p.P(`let eventType = "";`)
	p.P("let data: string[] = [];")
	p.P("try {")
	p.P("while (true) {")
	p.P("const { done, value } = await reader.read();")
	p.P("if (done) break;")
	p.P("buffer += decoder.decode(value, { stream: true });")
	p.P(`if (!buffer.includes("\n") && tooLong(buffer)) throw new Error("SSE line exceeds configured limit");`)
	p.P("let idx: number;")
	p.P(`while ((idx = buffer.indexOf("\n")) >= 0) {`)
	p.P(`const line = buffer.slice(0, idx).replace(/\r$/, "");`)
	p.P("buffer = buffer.slice(idx + 1);")
	p.P(`if (tooLong(line)) throw new Error("SSE line exceeds configured limit");`)
	p.P(`if (line === "") {`)
	p.P("const payload = data.join(\"\\n\");")
	p.P("const kind = eventType;")
	p.P(`eventType = "";`)
	p.P("const hasData = data.length > 0;")
	p.P("data = [];")
	p.P("if (!hasData) continue;")
	p.P(`if (kind === "error") throw new Error("stream error: " + payload);`)
	p.P("yield ", p.MessageCodecName(m.Response, "decode"), "(JSON.parse(payload));")
	p.P("continue;")
	p.P("}")
	p.P(`if (line.startsWith(":")) continue;`)
	p.P(`const colon = line.indexOf(":");`)
	p.P("const field = colon < 0 ? line : line.slice(0, colon);")
	p.P(`let fieldValue = colon < 0 ? "" : line.slice(colon + 1);`)
	p.P(`if (fieldValue.startsWith(" ")) fieldValue = fieldValue.slice(1);`)
	p.P(`if (field === "event") eventType = fieldValue;`)
	p.P(`else if (field === "data") data.push(fieldValue);`)
	p.P("}")
	p.P(`if (tooLong(buffer)) throw new Error("SSE line exceeds configured limit");`)
	p.P("}")
	p.P("} finally {")
	p.P("await reader.cancel().catch(() => {});")
	p.P("}")
}

// writeSSEClientMethod generates an async generator client method: SSE
// methods are consumed with `for await (const event of client.method(req))`.
// A preceding "event: error" line is thrown instead of yielded, mirroring the
// server's smart error handling on the client side.
func writeSSEClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.Path()
	fullPath := s.BasePath + path

	writeJSDoc(p, tsDeprecatedDoc(m.Doc, m.Deprecated))
	p.P("async *", CamelCase(m.Name), "(req: ", p.MessageTypeName(m.Request),
		", opts?: RequestOptions): AsyncGenerator<", p.MessageTypeName(m.Response), "> {")
	validator := p.MessageCodecName(m.Request, "validate")
	p.P("const violations = ", validator, "(req);")
	p.P(`if (violations.length > 0) throw new RequestValidationError("invalid request", violations);`)
	p.P(fmt.Sprintf("let path = %q;", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		if onkir.IsWildcardParam(path, paramName) {
			p.P(fmt.Sprintf(`if (String(req.%s).split("/").some((segment) => segment === "." || segment === "..")) throw new RequestValidationError("invalid request", [%q]);`,
				CamelCase(field.Name), paramName+": dot segments are not allowed"))
		}
		p.P(fmt.Sprintf(
			"path = path.replace(%q, %s);",
			onkir.PathPlaceholder(path, paramName), tsPathEncodeExpr(path, paramName, "req."+CamelCase(field.Name)),
		))
	}
	if verb, _ := m.Verb(); !onkir.IsBodyBearingVerb(verb) {
		writeClientQueryParams(p, m.Request)
	}

	writeSSEClientFetch(p, m)
	writeSSEClientReadLoop(p, m)

	p.P("}")
	p.P()
}
