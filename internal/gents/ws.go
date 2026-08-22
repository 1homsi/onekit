package gents

import (
	"fmt"
	"slices"

	"github.com/1homsi/onekit/internal/onkir"
)

// --- server ---------------------------------------------------------------

// WriteTSWSServerRuntime emits the shared WebSocket-server scaffolding: the
// WSOut sender interface, an upgrade helper built on the Web-standard
// WebSocketPair (Cloudflare Workers / Deno / Bun), and a socket route
// descriptor type. Node runtimes need a small adapter bridging 'ws' sockets
// onto the same handle function.
func WriteTSWSServerRuntime(p *Printer) {
	p.P("export interface WSOut<E> {")
	p.P("send(value: E): void | Promise<void>;")
	p.P("}")
	p.P()
	p.P("export interface SocketRouteDescriptor {")
	p.P("path: string;")
	p.P("handle: (req: Request, params: Record<string, string>) => Promise<Response> | Response;")
	p.P("}")
	p.P()
	p.P("// Minimal structural typing for the Web-standard socket pair; avoids a")
	p.P("// hard dependency on environment-specific DOM typings.")
	p.P("type PairServerSocket = {")
	p.P("accept(): void;")
	p.P("send(data: string): void;")
	p.P("close(code?: number, reason?: string): void;")
	p.P(`addEventListener(type: "message" | "close", listener: (event: any) => void): void;`)
	p.P("}")
	p.P()
}

func WriteTSWSSocketRoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	hasPathParams := len(onkir.PathParamNames(wsPath)) > 0

	p.P("{")
	p.P(fmt.Sprintf("path: %q,", fullPath))
	p.P("handle: async (req: Request, params: Record<string, string>): Promise<Response> => {")
	p.P("if ((req.headers.get(\"upgrade\") || \"\").toLowerCase() !== \"websocket\") {")
	p.P(`return new Response("expected websocket upgrade", { status: 426 });`)
	p.P("}")

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
	p.P("let body: Record<string, unknown> = {};")
	writeServerQueryParams(p, m.Request)
	if hasPathParams {
		for _, paramName := range onkir.PathParamNames(wsPath) {
			field := onkir.FindField(m.Request, paramName)
			if field == nil {
				continue
			}
			if field.Type != nil && field.Type.Kind == onkir.KindScalar {
				p.P("body.", field.Name, " = parseScalar(params.", paramName, ", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = params.", paramName, ";")
			}
		}
	}
	p.P("const connection = decode", m.Request.Name, "(body);")
	p.P("const connectionViolations = validate", m.Request.Name, "(connection);")
	p.P(`if (connectionViolations.length > 0) return new Response(JSON.stringify({ message: connectionViolations.join("; ") }), { status: 400, headers: { "Content-Type": "application/json" } });`)

	p.P("const pair = new (globalThis as any).WebSocketPair();")
	p.P("const server: PairServerSocket = pair.server;")
	p.P("server.accept();")
	p.P("const out: WSOut<", p.MessageTypeName(m.Response), "> = {")
	p.P("send: (value) => { server.send(JSON.stringify(value)); },")
	p.P("};")
	p.P("server.addEventListener(\"message\", async (event: any) => {")
	p.P("try {")
	p.P("const frame = decode", m.Request.Name, "(JSON.parse(String(event.data)));")
	p.P("const violations = validate", m.Request.Name, "(frame);")
	p.P("if (violations.length > 0) {")
	p.P(`server.send(JSON.stringify({ error: violations.join("; ") }));`)
	p.P("server.close(1008, \"invalid frame\");")
	p.P("return;")
	p.P("}")
	p.P("await handler.", CamelCase(m.Name), "(frame, out);")
	p.P("} catch (err) {")
	p.P(`server.send(JSON.stringify({ error: String(err) }));`)
	p.P("}")
	p.P("});")
	p.P("void handler.", CamelCase(m.Name), "(connection, out);")
	p.P("return new Response(null, { status: 101, webSocket: pair.client } as any);")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")
	p.P("},")
	p.P("},")
}

func writeTSSocketFactory(p *Printer, s *onkir.Service) {
	factory := "create" + s.Name + "SocketRoutes"
	p.P("export function ", factory, "(handler: ", s.Name, "Handler): SocketRouteDescriptor[] {")
	p.P("return [")
	for _, m := range s.Methods {
		if m.IsWebSocket() {
			WriteTSWSSocketRoute(p, s, m)
		}
	}
	p.P("];")
	p.P("}")
	p.P()
}

// --- client ---------------------------------------------------------------

// writeTSDuplexClass emits the browser-side duplex wrapper for one ws method
// pair: validated sends, promise-based receives, clean close.
func writeTSDuplexClass(p *Printer, m *onkir.Method) {
	name := tsDuplexName(m)
	reqRef := p.MessageTypeName(m.Request)
	resRef := p.MessageTypeName(m.Response)
	p.P("export class ", name, " {")
	p.P("constructor(private ws: WebSocket) {}")
	p.P()
	p.P("send(value: ", reqRef, "): void {")
	p.P("const frame = encode", m.Request.Name, "(value);")
	p.P("const violations = validate", m.Request.Name, "(frame);")
	p.P(`if (violations.length > 0) throw new TypeError("invalid frame: " + violations.join("; "));`)
	p.P("this.ws.send(JSON.stringify(frame));")
	p.P("}")
	p.P()
	p.P("receive(): Promise<", resRef, "> {")
	p.P("return new Promise((resolve, reject) => {")
	p.P("const onMessage = (event: MessageEvent) => { cleanup(); try { resolve(decode", m.Response.Name, "(JSON.parse(String(event.data)))); } catch (err) { reject(err); } };")
	p.P(`const onClose = () => { cleanup(); reject(new Error("websocket closed")); };`)
	p.P("const cleanup = () => { this.ws.removeEventListener(\"message\", onMessage); this.ws.removeEventListener(\"close\", onClose); };")
	p.P("this.ws.addEventListener(\"message\", onMessage);")
	p.P("this.ws.addEventListener(\"close\", onClose);")
	p.P("});")
	p.P("}")
	p.P()
	p.P("close(): void { this.ws.close(); }")
	p.P("}")
	p.P()
}

func tsDuplexName(m *onkir.Method) string {
	return PascalCase(m.Name) + "Socket"
}

func writeTSWSClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath

	p.P("async ", CamelCase(m.Name), "(req: ", p.MessageTypeName(m.Request), "): Promise<", tsDuplexName(m), "> {")
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(req);")
	p.P(`if (violations.length > 0) throw new TypeError("invalid request: " + violations.join("; "));`)
	p.P(fmt.Sprintf("let path = %q;", fullPath))
	for _, paramName := range onkir.PathParamNames(wsPath) {
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

	// http(s) -> ws(s)
	p.P(`let socketURL = this.baseUrl + path;`)
	p.P(`socketURL = socketURL.replace(/^https:/, "wss:").replace(/^http:/, "ws:");`)
	p.P("return new Promise((resolve, reject) => {")
	p.P("const ws = new WebSocket(socketURL);")
	p.P(`ws.onopen = () => resolve(new `, tsDuplexName(m), "(ws));")
	p.P(`ws.onerror = () => reject(new Error("websocket connection failed"));`)
	p.P("});")
	p.P("}")
	p.P()
}
