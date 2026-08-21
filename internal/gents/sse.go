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
	p.P("async function sseResponse<T>(stream: ReadableStream<T>, encode: (v: T) => unknown): Promise<Response> {")
	p.P("const reader = stream.getReader();")
	p.P("let first: ReadableStreamReadResult<T>;")
	p.P("try {")
	p.P("first = await reader.read();")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")
	p.P()
	p.P("const encoder = new TextEncoder();")
	p.P("const body = new ReadableStream<Uint8Array>({")
	p.P("async start(controller) {")
	p.P("let current = first;")
	p.P("try {")
	p.P("while (!current.done) {")
	p.P(`controller.enqueue(encoder.encode("data: " + JSON.stringify(encode(current.value)) + "\n\n"));`)
	p.P("current = await reader.read();")
	p.P("}")
	p.P("} catch (err) {")
	p.P(`const errBody = { message: "internal server error" };`)
	p.P(`controller.enqueue(encoder.encode("event: error\ndata: " + JSON.stringify(errBody) + "\n\n"));`)
	p.P("} finally {")
	p.P("controller.close();")
	p.P("}")
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
			p.P("if (!value) throw new HttpError(400, { message: ", fmt.Sprintf("%q", "missing required header: "+header.Name), " });")
		}
		if hasFormat {
			p.P("if (value && !validHeaderFormat(value, ", fmt.Sprintf("%q", format), ")) throw new HttpError(400, { message: ", fmt.Sprintf("%q", "invalid header "+header.Name+": expected "+format), " });")
		}
		p.P("}")
	}
	p.P("const body: Record<string, unknown> = {};")
	writeServerQueryParams(p, m.Request)
	if hasPathParams {
		for _, paramName := range onkir.PathParamNames(path) {
			field := onkir.FindField(m.Request, paramName)
			if field == nil {
				continue
			}
			if field.Type != nil && field.Type.Kind == onkir.KindScalar {
				p.P("body.", field.Name, " = parseScalar(match.", paramName, ", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = match.", paramName, ";")
			}
		}
	}

	p.P("const decoded = decode", m.Request.Name, "(body);")
	p.P("const violations = validate", m.Request.Name, "(decoded);")
	p.P("if (violations.length > 0) throw new HttpError(400, { message: violations.join(\"; \") });")
	p.P("const stream = handler.", CamelCase(m.Name), "(decoded, { request: req, headers: req.headers });")
	p.P("return await sseResponse(stream, encode", m.Response.Name, ");")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")

	p.P("},")
	p.P("},")
}

func writeSSEClientFetch(p *Printer, m *onkir.Method) {
	// The compiler restricts @stream to non-body-bearing verbs, so no request
	// body is ever attached here; the verb itself still follows the schema.
	verb, _ := m.Verb()
	p.P("const res = await this.request(this.baseUrl + path, {")
	p.P(fmt.Sprintf("method: %q,", strings.ToUpper(verb)))
	p.P(`headers: { Accept: "text/event-stream", ...this.options.defaultHeaders },`)
	p.P("signal: opts?.signal,")
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
	p.P(`let buffer = "";`)
	p.P(`let pendingEvent = "";`)
	p.P("while (true) {")
	p.P("const { done, value } = await reader.read();")
	p.P("if (done) break;")
	p.P("buffer += decoder.decode(value, { stream: true });")
	p.P(`if (!buffer.includes("\n") && new TextEncoder().encode(buffer).byteLength > maxSSELineBytes) { await reader.cancel(); throw new Error("SSE line exceeds configured limit"); }`)
	p.P("let idx: number;")
	p.P(`while ((idx = buffer.indexOf("\n")) >= 0) {`)
	p.P(`const line = buffer.slice(0, idx).replace(/\r$/, "");`)
	p.P("buffer = buffer.slice(idx + 1);")
	p.P(`if (new TextEncoder().encode(line).byteLength > maxSSELineBytes) { await reader.cancel(); throw new Error("SSE line exceeds configured limit"); }`)
	p.P(`if (line.startsWith("event:")) {`)
	p.P("pendingEvent = line.slice(6).trim();")
	p.P("continue;")
	p.P("}")
	p.P(`if (!line.startsWith("data:")) {`)
	p.P("continue;")
	p.P("}")
	p.P("const data = line.slice(5).trim();")
	p.P("const eventType = pendingEvent;")
	p.P(`pendingEvent = "";`)
	p.P(`if (eventType === "error") {`)
	p.P(`throw new Error("stream error: " + data);`)
	p.P("}")
	p.P("yield decode", m.Response.Name, "(JSON.parse(data));")
	p.P("}")
	p.P(`if (new TextEncoder().encode(buffer).byteLength > maxSSELineBytes) { await reader.cancel(); throw new Error("SSE line exceeds configured limit"); }`)
	p.P("}")
}

// writeSSEClientMethod generates an async generator client method: SSE
// methods are consumed with `for await (const event of client.method(req))`.
// A preceding "event: error" line is thrown instead of yielded, mirroring the
// server's smart error handling on the client side.
func writeSSEClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	path, _ := m.Path()
	fullPath := s.BasePath + path

	p.P("async *", CamelCase(m.Name), "(req: ", p.MessageTypeName(m.Request),
		", opts?: { signal?: AbortSignal }): AsyncGenerator<", p.MessageTypeName(m.Response), "> {")
	validator := p.MessageCodecName(m.Request, "validate")
	p.P("const violations = ", validator, "(req);")
	p.P(`if (violations.length > 0) throw new TypeError("invalid request: " + violations.join("; "));`)
	p.P(fmt.Sprintf("let path = %q;", fullPath))
	for _, paramName := range onkir.PathParamNames(path) {
		field := onkir.FindField(m.Request, paramName)
		if field == nil {
			continue
		}
		p.P(fmt.Sprintf(
			"path = path.replace(%q, encodeURIComponent(String(req.%s)));",
			"{"+paramName+"}", CamelCase(field.Name),
		))
	}
	writeClientQueryParams(p, m.Request)

	writeSSEClientFetch(p, m)
	writeSSEClientReadLoop(p, m)

	p.P("}")
	p.P()
}
