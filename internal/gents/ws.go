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
// descriptor type. Plain Node has no WebSocketPair at all - see
// WriteTSWSNodeServerRuntime/writeTSNodeSocketFactory for the adapter built
// on the `ws` package that bridges the same handler/out/correlation shape
// onto a real Node http.Server's 'upgrade' event.
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
	p.P("readyState: number;")
	p.P("accept(): void;")
	p.P("send(data: string): void;")
	p.P("close(code?: number, reason?: string): void;")
	p.P(`addEventListener(type: "message" | "close", listener: (event: any) => void): void;`)
	p.P("}")
	p.P()
}

// WriteTSWSNodeServerRuntime emits the one helper the Node adapter needs
// beyond what WriteTSWSServerRuntime already provides (WSOut/WSCallOut/
// WSPending and the rest are already platform-agnostic): a header reader
// matching req.headers.get()'s shape against Node's IncomingHttpHeaders,
// which lower-cases header names and can return string | string[].
func WriteTSWSNodeServerRuntime(p *Printer) {
	p.P("function nodeHeaderValue(headers: IncomingHttpHeaders, name: string): string | null {")
	p.P("const value = headers[name.toLowerCase()];")
	p.P("if (Array.isArray(value)) return value[0] ?? null;")
	p.P("return value ?? null;")
	p.P("}")
	p.P()
	p.P("type NodeSocketRoute = (req: IncomingMessage, socket: Duplex, head: Buffer, url: URL) => boolean;")
	p.P()
	p.P("// One 'upgrade' listener per http.Server, shared by every")
	p.P("// attach*NodeSocketHandlers call - including ones from other generated")
	p.P("// modules, via the global Symbol.for registry - so an upgrade whose path")
	p.P("// no route claims can be rejected instead of hanging until TCP timeout.")
	p.P(`const nodeSocketRoutesKey = Symbol.for("onekit.nodeSocketRoutes");`)
	p.P()
	p.P("function registerNodeSocketRoute(httpServer: HttpServer, route: NodeSocketRoute): void {")
	p.P("const holder = httpServer as unknown as Record<symbol, NodeSocketRoute[] | undefined>;")
	p.P("const existing = holder[nodeSocketRoutesKey];")
	p.P("if (existing) { existing.push(route); return; }")
	p.P("const routes: NodeSocketRoute[] = [route];")
	p.P("holder[nodeSocketRoutesKey] = routes;")
	p.P(`httpServer.on("upgrade", (req: IncomingMessage, socket: Duplex, head: Buffer) => {`)
	p.P(`const url = new URL(req.url ?? "/", "http://" + (req.headers.host ?? "localhost"));`)
	p.P("for (const r of routes) if (r(req, socket, head, url)) return;")
	p.P("// Another upgrade listener (socket.io, a hand-written route) may own this")
	p.P("// path; only reject when nothing else could.")
	p.P(`if (httpServer.listenerCount("upgrade") > 1) return;`)
	p.P(`socket.write("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n");`)
	p.P("socket.destroy();")
	p.P("});")
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
	p.P("private closedWith: unknown = null;")
	p.P("private isClosed = false;")
	p.P()
	p.P("// closed is sticky: once the connection is gone, a later register()")
	p.P("// rejects immediately instead of waiting on a reply that can never come.")
	p.P("get closed(): boolean { return this.isClosed; }")
	p.P()
	p.P("register(id: K): Promise<T> {")
	p.P("if (this.isClosed) return Promise.reject(this.closedWith);")
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
	p.P("this.isClosed = true;")
	p.P("this.closedWith = err;")
	p.P("for (const waiter of this.waiters.values()) waiter.reject(err);")
	p.P("this.waiters.clear();")
	p.P("}")
	p.P("}")
	p.P()
}

// tsWSIDExpression builds an expression extracting whichever field of
// message actually carries @ws_id - a direct field, or (independently, per
// variant) any oneof variant whose own message carries one. idField only
// supplies the shared TS type for the expression's return type: onkcompile
// guarantees every @ws_id field a method touches shares one scalar type,
// but each oneof variant has its own distinct field (e.g. HostCall.id vs
// HostResult.id) with its own name, so - unlike an earlier version of this
// function - it must not filter variants by comparing against idField's
// identity: doing so only ever matched whichever single field
// onkir.WSIDField(message) happened to return first (declaration order),
// silently generating no extraction code at all for every other variant.
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
				if !ok {
					continue
				}
				variantProp := fieldAccess
				if !flatten {
					variantProp = fieldAccess + "." + CamelCase(variant.Name)
				}
				fmt.Fprintf(&b, "if (%s && %s.%s === %q) return %s.%s;\n",
					fieldAccess, fieldAccess, disc, variant.Tag(), variantProp, CamelCase(vf.Name))
			}
			continue
		}
		if !f.HasDecorator("ws_id") {
			continue
		}
		fmt.Fprintf(&b, "return %s.%s;\n", frameExpr, CamelCase(f.Name))
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
	p.P("const url = new URL(req.url);")
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
	writeTSWSSocketBody(p, m, "server")
	p.P("return new Response(null, { status: 101, webSocket: pair.client } as any);")
	p.P("} catch (err) {")
	p.P("return errorResponse(err);")
	p.P("}")
	p.P("},")
	p.P("},")
}

// writeTSWSSocketBody emits the platform-independent core of a @ws server
// handler: out/pending construction, the inbound-message handler,
// correlation extraction, and the initial connect-time handler call. It's
// shared between the Workers-style route (socketVar "server", typed
// PairServerSocket) and the Node adapter (socketVar "ws", typed by the real
// `ws` package via inference) since neither WSOut/WSCallOut/WSPending nor
// this body touches any Workers-specific API - only the surrounding
// request/upgrade plumbing differs per platform.
func writeTSWSSocketBody(p *Printer, m *onkir.Method, socketVar string) {
	idField, correlated := m.WSIDField()
	// Outgoing frames go through encode<Response> exactly like the TS client's
	// send() does, so the wire carries the schema's own keys (host_call,
	// exit_code) rather than the decoded camelCase TS shape - a Go or Rust
	// peer decoding a camelCase frame sees the oneof tag but a nil body.
	sendFrame := socketVar + ".send(JSON.stringify(" + p.MessageCodecName(m.Response, "encode") + "(value)))"
	if correlated {
		idType := p.TSFieldType(idField.Type)
		p.P("const pending = new WSPending<", idType, ", ", p.MessageTypeName(m.Request), ">();")
		p.P("const out: WSCallOut<", idType, ", ", p.MessageTypeName(m.Response), ", ", p.MessageTypeName(m.Request), "> = {")
		p.P("send: (value) => { ", sendFrame, "; },")
		// A socket that's closing or closed silently drops sends, so without
		// this a call() made after the peer left would never settle.
		p.P("call: (id, value) => {")
		p.P("if (", socketVar, `.readyState !== 1) pending.rejectAll(new Error("websocket closed"));`)
		p.P("const reply = pending.register(id);")
		p.P("if (!pending.closed) ", sendFrame, ";")
		p.P("return reply;")
		p.P("},")
		p.P("};")
		p.P(socketVar, `.addEventListener("close", () => { pending.rejectAll(new Error("websocket closed")); });`)
	} else {
		p.P("const out: WSOut<", p.MessageTypeName(m.Response), "> = {")
		p.P("send: (value) => { ", sendFrame, "; },")
		p.P("};")
	}
	p.P(socketVar, ".addEventListener(\"message\", async (event: any) => {")
	p.P("try {")
	p.P("const frame = ", p.MessageCodecName(m.Request, "decode"), "(JSON.parse(String(event.data)));")
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(frame);")
	p.P("if (violations.length > 0) {")
	p.P(socketVar, `.send(JSON.stringify({ error: violations.join("; ") }));`)
	p.P(socketVar, ".close(1008, \"invalid frame\");")
	p.P("return;")
	p.P("}")
	if correlated {
		p.P("const replyId = ", tsWSIDExpression(p, "frame", m.Request, idField), ";")
		p.P("if (replyId !== undefined && pending.resolve(replyId, frame)) return;")
	}
	p.P("await handler.", CamelCase(m.Name), "(frame, out);")
	p.P("} catch (err) {")
	p.P(socketVar, `.send(JSON.stringify({ error: String(err) }));`)
	p.P("}")
	p.P("});")
	p.P("void handler.", CamelCase(m.Name), "(connection, out);")
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

// writeTSNodeSocketFactory emits attach<Service>NodeSocketHandlers, the
// Node counterpart to create<Service>SocketRoutes: safe to call once per
// service on the same http.Server (Node allows multiple 'upgrade'
// listeners; each checks its own paths via matchPath and only acts on a
// match), since a single 'upgrade' event has no per-request return value
// for a consumer to dispatch on the way SocketRouteDescriptor[] assumes.
func writeTSNodeSocketFactory(p *Printer, s *onkir.Service) {
	factory := "attach" + s.Name + "NodeSocketHandlers"
	p.P("export function ", factory, "(httpServer: HttpServer, handler: ", s.Name, "Handler): void {")
	p.P("const wss = new WebSocketServer({ noServer: true });")
	p.P("registerNodeSocketRoute(httpServer, (req, socket, head, url) => {")
	for _, m := range s.Methods {
		if m.IsWebSocket() {
			WriteTSWSNodeSocketRoute(p, s, m)
		}
	}
	p.P("return false;")
	p.P("});")
	p.P("}")
	p.P()
}

// WriteTSWSNodeSocketRoute emits one path-matched block inside the shared
// 'upgrade' listener writeTSNodeSocketFactory builds. Everything from
// connection decode onward reuses writeTSWSSocketBody - identical to the
// Workers-style route - since only the surrounding request/upgrade
// mechanics differ: Node's http.IncomingMessage headers instead of a Fetch
// Request, matchPath against a raw net.Socket instead of a router-supplied
// params object, and wss.handleUpgrade instead of returning a Response.
func WriteTSWSNodeSocketRoute(p *Printer, s *onkir.Service, m *onkir.Method) {
	wsPath, _ := m.WebSocketPath()
	fullPath := s.BasePath + wsPath
	hasPathParams := len(onkir.PathParamNames(wsPath)) > 0

	p.P("{")
	p.P("const match = matchPath(", fmt.Sprintf("%q", fullPath), ", url.pathname);")
	p.P("if (match) {")
	p.P("try {")
	for _, header := range slices.Concat(s.Headers, m.Headers) {
		format, hasFormat := header.Format()
		p.P("{")
		p.P("const value = nodeHeaderValue(req.headers, ", fmt.Sprintf("%q", header.Name), ");")
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
				p.P("body.", field.Name, " = parseScalar(match.", paramName, ", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = match.", paramName, ";")
			}
		}
	}
	p.P("const connection = ", p.MessageCodecName(m.Request, "decode"), "(body);")
	p.P("const connectionViolations = ", p.MessageCodecName(m.Request, "validate"), "(connection);")
	p.P("if (connectionViolations.length > 0) {")
	p.P(`socket.write("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n");`)
	p.P("socket.destroy();")
	p.P("return true;")
	p.P("}")
	p.P("wss.handleUpgrade(req, socket, head, (ws) => {")
	writeTSWSSocketBody(p, m, "ws")
	p.P("});")
	p.P("} catch (err) {")
	p.P("const status = err instanceof HttpError ? err.status : 400;")
	p.P(`socket.write("HTTP/1.1 " + status + " Bad Request\r\nConnection: close\r\n\r\n");`)
	p.P("socket.destroy();")
	p.P("}")
	p.P("return true;")
	p.P("}")
	p.P("}")
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
	// The close listener only exists once ensureListening() has run, so a
	// socket that closed before the first call() never marked pending closed;
	// check readyState directly rather than sending into a dead socket.
	p.P(`if (this.ws.readyState !== 1) this.pending.rejectAll(this.closedWith ?? new Error("websocket closed"));`)
	p.P("const reply = this.pending.register(id);")
	p.P("if (this.pending.closed) return reply;")
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
