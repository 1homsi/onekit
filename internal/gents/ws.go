package gents

import (
	"fmt"
	"slices"
	"strings"

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
	writeTSWSPendingType(p)
	p.P("// WSCallOut extends WSOut with Call: send a correlated frame and await")
	p.P("// the reply carrying the matching @ws_id, resolved by the read loop.")
	p.P("export interface WSCallOut<K, E, R> extends WSOut<E> {")
	p.P("call(id: K, value: E): Promise<R>;")
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

// writeTSWSPendingType emits the correlation-map runtime shared by every
// @ws_id-using handler and duplex class in the file: register(id) hands back
// a promise that resolve(id, value) fulfills exactly once, so a concurrent
// call can await a specific reply among many interleaved frames. TS modules
// are file-scoped, so - unlike the Go backend - client and server can share
// this one name even when generated into the same directory.
func writeTSWSPendingType(p *Printer) {
	p.P("// WSPending tracks in-flight correlated WebSocket calls, keyed by an")
	p.P("// application-supplied @ws_id value, so multiple calls can be")
	p.P("// outstanding at once on a single connection and resolved out of order.")
	p.P("export class WSPending<K, T> {")
	p.P("private waiters = new Map<K, { resolve: (value: T) => void; reject: (err: unknown) => void }>();")
	p.P()
	p.P("register(id: K): Promise<T> {")
	p.P("return new Promise((resolve, reject) => { this.waiters.set(id, { resolve, reject }); });")
	p.P("}")
	p.P()
	p.P("resolve(id: K, value: T): boolean {")
	p.P("const waiter = this.waiters.get(id);")
	p.P("if (!waiter) return false;")
	p.P("this.waiters.delete(id);")
	p.P("waiter.resolve(value);")
	p.P("return true;")
	p.P("}")
	p.P()
	p.P("cancel(id: K): void { this.waiters.delete(id); }")
	p.P()
	p.P("rejectAll(err: unknown): void {")
	p.P("for (const waiter of this.waiters.values()) waiter.reject(err);")
	p.P("this.waiters.clear();")
	p.P("}")
	p.P("}")
	p.P()
}

// tsWSIDExpression returns a TS IIFE expression evaluating to the @ws_id
// value found within frameExpr (typed as message, already decoded), or
// undefined - checking direct fields first, then each oneof variant's own
// message, mirroring decodeOneofExpr's wire shape (types.go) for both the
// flattened and nested-under-variant-key cases.
func tsWSIDExpression(p *Printer, frameExpr string, message *onkir.Message, idField *onkir.Field) string {
	idType := p.TSFieldType(idField.Type)
	var b strings.Builder
	fmt.Fprintf(&b, "((): %s | undefined => {\n", idType)
	for _, f := range message.Fields {
		if f.Oneof != nil {
			disc := oneofDiscriminatorKey(f)
			flatten := f.Oneof.Flatten()
			fieldAccess := frameExpr + "." + CamelCase(f.Name)
			for _, variant := range f.Oneof.Variants {
				if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
					continue
				}
				vf, ok := onkir.WSIDField(variant.Type.Message)
				if !ok || vf != idField {
					continue
				}
				variantProp := fieldAccess
				if !flatten {
					variantProp = fieldAccess + "." + CamelCase(variant.Name)
				}
				fmt.Fprintf(&b, "if (%s && %s.%s === %q) return %s.%s;\n",
					fieldAccess, fieldAccess, disc, variant.Tag(), variantProp, CamelCase(idField.Name))
			}
			continue
		}
		if f != idField {
			continue
		}
		fmt.Fprintf(&b, "return %s.%s;\n", frameExpr, CamelCase(idField.Name))
	}
	b.WriteString("return undefined;\n")
	b.WriteString("})()")
	return b.String()
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
	idField, correlated := m.WSIDField()
	if correlated {
		idType := p.TSFieldType(idField.Type)
		p.P("const pending = new WSPending<", idType, ", ", p.MessageTypeName(m.Request), ">();")
		p.P("const out: WSCallOut<", idType, ", ", p.MessageTypeName(m.Response), ", ", p.MessageTypeName(m.Request), "> = {")
		p.P("send: (value) => { server.send(JSON.stringify(value)); },")
		p.P("call: (id, value) => { const reply = pending.register(id); server.send(JSON.stringify(value)); return reply; },")
		p.P("};")
		p.P(`server.addEventListener("close", () => { pending.rejectAll(new Error("websocket closed")); });`)
	} else {
		p.P("const out: WSOut<", p.MessageTypeName(m.Response), "> = {")
		p.P("send: (value) => { server.send(JSON.stringify(value)); },")
		p.P("};")
	}
	p.P("server.addEventListener(\"message\", async (event: any) => {")
	p.P("try {")
	p.P("const frame = decode", m.Request.Name, "(JSON.parse(String(event.data)));")
	p.P("const violations = validate", m.Request.Name, "(frame);")
	p.P("if (violations.length > 0) {")
	p.P(`server.send(JSON.stringify({ error: violations.join("; ") }));`)
	p.P("server.close(1008, \"invalid frame\");")
	p.P("return;")
	p.P("}")
	if correlated {
		p.P("const replyId = ", tsWSIDExpression(p, "frame", m.Request, idField), ";")
		p.P("if (replyId !== undefined && pending.resolve(replyId, frame)) return;")
	}
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
// pair: validated sends, clean close, and either the original one-shot
// promise-based receive() (no @ws_id in play) or - when the method's request
// or response carries @ws_id - a persistent listener that routes correlated
// replies to call() and everything else to receive(), so both can be used
// concurrently without racing on the socket's message event.
func writeTSDuplexClass(p *Printer, m *onkir.Method) {
	name := tsDuplexName(m)
	reqRef := p.MessageTypeName(m.Request)
	resRef := p.MessageTypeName(m.Response)
	idField, correlated := m.WSIDField()

	p.P("export class ", name, " {")
	if correlated {
		idType := p.TSFieldType(idField.Type)
		p.P("private pending = new WSPending<", idType, ", ", resRef, ">();")
		p.P("private inboxQueue: ", resRef, "[] = [];")
		p.P("private inboxWaiters: Array<{ resolve: (value: ", resRef, ") => void; reject: (err: unknown) => void }> = [];")
		p.P("private closedWith: unknown = undefined;")
		p.P("private listening = false;")
	}
	p.P("constructor(private ws: WebSocket) {}")
	p.P()
	p.P("send(value: ", reqRef, "): void {")
	p.P("const frame = encode", m.Request.Name, "(value);")
	p.P("const violations = validate", m.Request.Name, "(frame);")
	p.P(`if (violations.length > 0) throw new TypeError("invalid frame: " + violations.join("; "));`)
	p.P("this.ws.send(JSON.stringify(frame));")
	p.P("}")
	p.P()

	if !correlated {
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
		return
	}

	p.P("private ensureListening(): void {")
	p.P("if (this.listening) return;")
	p.P("this.listening = true;")
	p.P("this.ws.addEventListener(\"message\", (event: MessageEvent) => {")
	p.P("let frame: ", resRef, ";")
	p.P("try { frame = decode", m.Response.Name, "(JSON.parse(String(event.data))); } catch { return; }")
	p.P("const replyId = ", tsWSIDExpression(p, "frame", m.Response, idField), ";")
	p.P("if (replyId !== undefined && this.pending.resolve(replyId, frame)) return;")
	p.P("const waiter = this.inboxWaiters.shift();")
	p.P("if (waiter) { waiter.resolve(frame); return; }")
	p.P("this.inboxQueue.push(frame);")
	p.P("});")
	p.P(`this.ws.addEventListener("close", () => {`)
	p.P(`this.closedWith = new Error("websocket closed");`)
	p.P("this.pending.rejectAll(this.closedWith);")
	p.P("for (const waiter of this.inboxWaiters.splice(0)) waiter.reject(this.closedWith);")
	p.P("});")
	p.P("}")
	p.P()
	p.P("receive(): Promise<", resRef, "> {")
	p.P("this.ensureListening();")
	p.P("const queued = this.inboxQueue.shift();")
	p.P("if (queued !== undefined) return Promise.resolve(queued);")
	p.P("if (this.closedWith !== undefined) return Promise.reject(this.closedWith);")
	p.P("return new Promise((resolve, reject) => { this.inboxWaiters.push({ resolve, reject }); });")
	p.P("}")
	p.P()
	p.P("// call sends value, then resolves once a response-direction frame")
	p.P("// carrying the matching @ws_id arrives, or rejects if the connection")
	p.P("// closes first. Safe alongside receive(): the persistent listener")
	p.P("// routes correlated replies here and everything else to it.")
	p.P("call(id: ", p.TSFieldType(idField.Type), ", value: ", reqRef, "): Promise<", resRef, "> {")
	p.P("this.ensureListening();")
	p.P("const reply = this.pending.register(id);")
	p.P("try {")
	p.P("this.send(value);")
	p.P("} catch (err) {")
	p.P("this.pending.cancel(id);")
	p.P("return Promise.reject(err);")
	p.P("}")
	p.P("return reply;")
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
