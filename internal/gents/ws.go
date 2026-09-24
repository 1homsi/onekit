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
	p.P("readonly signal: AbortSignal;")
	p.P("close(code?: number, reason?: string): void;")
	p.P("}")
	p.P()
	writeTSWSSharedRuntime(p)
	writeTSWSPendingType(p)
	p.P("// WSCallOut extends WSOut with Call: send a correlated frame and await")
	p.P("// the reply carrying the matching @ws_id, resolved by the read loop.")
	p.P("export interface WSCallOut<K, E, R> extends WSOut<E> {")
	p.P("call(id: K, value: E, options?: WSCallOptions): Promise<R>;")
	p.P("}")
	p.P()
	p.P("export interface WSServerOptions {")
	p.P("// Cap on one inbound message (default DEFAULT_MAX_WS_FRAME_BYTES); a")
	p.P("// larger one closes the connection with 1009. Negative disables it.")
	p.P("maxFrameBytes?: number;")
	p.P("maxMessageBytes?: number;")
	p.P("// out.send() waits while more than this many bytes are queued on the")
	p.P("// socket (default DEFAULT_WS_HIGH_WATER_MARK_BYTES).")
	p.P("highWaterMarkBytes?: number;")
	p.P("pingIntervalMs?: number;")
	p.P("}")
	p.P()
	p.P("// wsCloseReason fits message into a close frame's 123-byte reason, cutting")
	p.P("// on a UTF-8 boundary (the socket would throw on a longer one).")
	p.P("function wsCloseReason(message: string): string {")
	p.P("const bytes = new TextEncoder().encode(message);")
	p.P("if (bytes.byteLength <= 123) return message;")
	p.P("let cut = 123;")
	p.P("while (cut > 0 && ((bytes[cut] ?? 0) & 0xc0) === 0x80) cut--;")
	p.P("return new TextDecoder().decode(bytes.subarray(0, cut));")
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
	p.P("bufferedAmount?: number;")
	p.P("accept(): void;")
	p.P("send(data: string | ArrayBufferView | ArrayBuffer): void;")
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
	p.P("let url: URL;")
	p.P(`try { url = new URL(req.url ?? "/", "http://" + (req.headers.host ?? "localhost")); } catch { socket.write("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"); socket.destroy(); return; }`)
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

// writeTSWSSharedRuntime emits what both generated WS sides use: typed
// errors a caller can branch on with instanceof, per-call options, and the
// inbound frame-size check (Workers and browsers have no built-in cap).
func writeTSWSSharedRuntime(p *Printer) {
	p.P("// WSClosedError: the connection is gone. code/reason come from the close")
	p.P("// frame when there was one (1009 means a frame exceeded maxFrameBytes).")
	p.P("export class WSClosedError extends Error {")
	p.P("constructor(readonly code?: number, readonly closeReason?: string) {")
	p.P(`super(code === undefined ? "websocket closed" : "websocket closed (" + code + (closeReason ? ": " + closeReason : "") + ")");`)
	p.P(`this.name = "WSClosedError";`)
	p.P("}")
	p.P("}")
	p.P()
	p.P("// WSTimeoutError: call() gave up after timeoutMs, or its signal was")
	p.P("// aborted with a TimeoutError (AbortSignal.timeout).")
	p.P("export class WSTimeoutError extends Error {")
	p.P(`constructor() { super("websocket call timed out"); this.name = "WSTimeoutError"; }`)
	p.P("}")
	p.P()
	p.P("// WSCancelledError: call()'s signal was aborted.")
	p.P("export class WSCancelledError extends Error {")
	p.P(`constructor() { super("websocket call cancelled"); this.name = "WSCancelledError"; }`)
	p.P("}")
	p.P()
	p.P("// WSCallOptions bound one correlated call. When either fires, the call")
	p.P("// rejects and - if the schema declares a @ws_cancel variant - the peer is")
	p.P("// sent a cancel frame carrying the call's @ws_id.")
	p.P("export interface WSCallOptions {")
	p.P("signal?: AbortSignal;")
	p.P("timeoutMs?: number;")
	p.P("}")
	p.P()
	p.P("// Inbound message cap applied unless maxFrameBytes says otherwise; the")
	p.P("// same default every onekit target uses. A negative limit disables it.")
	p.P("export const DEFAULT_MAX_WS_FRAME_BYTES = 16 * 1024 * 1024;")
	p.P()
	p.P("// Bytes a socket may have queued before send() starts waiting.")
	p.P("export const DEFAULT_WS_HIGH_WATER_MARK_BYTES = 1024 * 1024;")
	p.P()
	p.P("// wsDrained resolves once socket's send buffer is at or under")
	p.P("// highWaterMark, or the socket is no longer open. It never rejects, so an")
	p.P("// un-awaited send() can't raise an unhandled rejection.")
	p.P("function wsDrained(socket: { readyState: number; bufferedAmount?: number }, highWaterMark: number): Promise<void> {")
	p.P("const ready = () => socket.readyState !== 1 || (socket.bufferedAmount ?? 0) <= highWaterMark;")
	p.P("if (ready()) return Promise.resolve();")
	p.P("return new Promise((resolve) => {")
	p.P("const check = () => { if (ready()) resolve(); else setTimeout(check, 10); };")
	p.P("setTimeout(check, 10);")
	p.P("});")
	p.P("}")
	p.P()
	p.P("function wsAbortError(signal: AbortSignal): Error {")
	p.P("const reason: unknown = signal.reason;")
	p.P(`const isTimeout = typeof reason === "object" && reason !== null && (reason as { name?: unknown }).name === "TimeoutError";`)
	p.P("return isTimeout ? new WSTimeoutError() : new WSCancelledError();")
	p.P("}")
	p.P()
	p.P("function wsFrameTooLarge(data: unknown, limit: number): boolean {")
	p.P("if (limit < 0) return false;")
	p.P(`if (typeof data === "string") {`)
	p.P("// UTF-8 needs at least one byte per UTF-16 unit and at most three, so")
	p.P("// only encode when the length alone can't decide.")
	p.P("if (data.length > limit) return true;")
	p.P("if (data.length * 3 <= limit) return false;")
	p.P("return new TextEncoder().encode(data).byteLength > limit;")
	p.P("}")
	p.P("const sized = data as { byteLength?: number; size?: number } | null;")
	p.P("return (sized?.byteLength ?? sized?.size ?? 0) > limit;")
	p.P("}")
	p.P()
}

// tsWSCancelFrame builds the serialized @ws_cancel frame of message carrying
// idExpr, if the schema declares one. The literal names only the oneof and
// the id, so it goes through `as unknown as` rather than requiring every
// other field of the frame to be spelled out.
func tsWSCancelFrame(p *Printer, message *onkir.Message, idExpr string) (string, bool) {
	f, variant, idField, ok := onkir.WSCancelVariant(message)
	if !ok {
		return "", false
	}
	disc := oneofDiscriminatorKey(f)
	idProp := CamelCase(idField.Name) + ": " + idExpr
	payload := fmt.Sprintf("{ %s: %q, %s: { %s } }", disc, variant.Tag(), CamelCase(variant.Name), idProp)
	if f.Oneof.Flatten() {
		payload = fmt.Sprintf("{ %s: %q, %s }", disc, variant.Tag(), idProp)
	}
	return "JSON.stringify(" + p.MessageCodecName(message, "encode") + "({ " + CamelCase(f.Name) + ": " + payload +
		" } as unknown as " + p.MessageTypeName(message) + "))", true
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
	p.P("// sent is the oneof variant tag of the frame each call sent (\"\" when not")
	p.P("// a oneof); see resolve.")
	p.P("private waiters = new Map<K, { sent: string; resolve: (value: T) => void; reject: (err: unknown) => void }>();")
	p.P("private closedWith: unknown = null;")
	p.P("private isClosed = false;")
	p.P()
	p.P("// closed is sticky: once the connection is gone, a later register()")
	p.P("// rejects immediately instead of waiting on a reply that can never come.")
	p.P("get closed(): boolean { return this.isClosed; }")
	p.P()
	p.P("// register returns a promise for id's reply. options.timeoutMs and")
	p.P("// options.signal abandon the wait (WSTimeoutError/WSCancelledError), and")
	p.P("// onAbandon then runs so the caller can tell the peer.")
	p.P("register(id: K, sent: string, options: WSCallOptions = {}, onAbandon?: () => void): Promise<T> {")
	p.P("if (this.isClosed) return Promise.reject(this.closedWith);")
	p.P("const signal = options.signal;")
	p.P("if (signal?.aborted) return Promise.reject(wsAbortError(signal));")
	p.P("return new Promise<T>((resolve, reject) => {")
	p.P("let timer: ReturnType<typeof setTimeout> | undefined;")
	p.P("const settle = () => {")
	p.P("if (timer !== undefined) clearTimeout(timer);")
	p.P(`signal?.removeEventListener("abort", onAbort);`)
	p.P("};")
	p.P("const entry = {")
	p.P("sent,")
	p.P("resolve: (value: T) => { settle(); resolve(value); },")
	p.P("reject: (err: unknown) => { settle(); reject(err); },")
	p.P("};")
	p.P("const abandon = (err: unknown) => {")
	p.P("if (this.waiters.get(id) !== entry) return;")
	p.P("this.waiters.delete(id);")
	p.P("entry.reject(err);")
	p.P("onAbandon?.();")
	p.P("};")
	p.P("const onAbort = () => { if (signal) abandon(wsAbortError(signal)); };")
	p.P("this.waiters.set(id, entry);")
	p.P("if (options.timeoutMs !== undefined) timer = setTimeout(() => abandon(new WSTimeoutError()), options.timeoutMs);")
	p.P(`signal?.addEventListener("abort", onAbort, { once: true });`)
	p.P("});")
	p.P("}")
	p.P()
	p.P("// resolve hands value to the call waiting on id, unless value is the same")
	p.P("// oneof variant that call sent: that is the peer starting its own call")
	p.P("// under a colliding id, not a reply, so it goes to the handler/receive().")
	p.P("resolve(id: K, variant: string, value: T): boolean {")
	p.P("const waiter = this.waiters.get(id);")
	p.P("if (!waiter) return false;")
	p.P(`if (waiter.sent !== "" && waiter.sent === variant) return false;`)
	p.P("this.waiters.delete(id);")
	p.P("waiter.resolve(value);")
	p.P("return true;")
	p.P("}")
	p.P()
	p.P("// fail rejects id's call with err, e.g. when sending it threw.")
	p.P("fail(id: K, err: unknown): void {")
	p.P("const waiter = this.waiters.get(id);")
	p.P("if (!waiter) return;")
	p.P("this.waiters.delete(id);")
	p.P("waiter.reject(err);")
	p.P("}")
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
				// A cancel is never a reply: it goes to the handler/receive().
				if variant.IsWSCancel() || variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
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

// tsWSVariantExpression builds an expression evaluating to frameExpr's oneof
// variant tag among the variants that carry @ws_id, or "" - what a call's
// reply must differ from (see WSPending.resolve).
func tsWSVariantExpression(frameExpr string, message *onkir.Message) string {
	var b strings.Builder
	b.WriteString("((): string => {\n")
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		disc := oneofDiscriminatorKey(f)
		fieldAccess := frameExpr + "." + CamelCase(f.Name)
		for _, variant := range f.Oneof.Variants {
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
				continue
			}
			if _, ok := onkir.WSIDField(variant.Type.Message); !ok {
				continue
			}
			fmt.Fprintf(&b, "if (%s && %s.%s === %q) return %q;\n", fieldAccess, fieldAccess, disc, variant.Tag(), variant.Tag())
		}
	}
	b.WriteString(`return "";` + "\n")
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
				p.P("body.", field.Name, " = parseScalar(params[", fmt.Sprintf("%q", paramName), "] ?? \"\", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = params[", fmt.Sprintf("%q", paramName), "] ?? \"\";")
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
	respSplit, _ := p.wsRawCodecArgs(m.Response)
	_, reqJoin := p.wsRawCodecArgs(m.Request)
	sendFrame := "wsSend(" + socketVar + ", wsEncodeMessage(value, " + p.MessageCodecName(m.Response, "encode") + ", " + respSplit + "))"
	// send resolves once the socket's buffer is back under the high-water
	// mark, so a producer that awaits it is paced by the peer.
	sendDrained := "send: (value) => { " + sendFrame + "; return wsDrained(" + socketVar + ", highWaterMark); },"
	p.P("const closed = new AbortController();")
	p.P("const assembler = new WSAssembler(maxMessageBytes);")
	if correlated {
		idType := p.TSFieldType(idField.Type)
		p.P("const pending = new WSPending<", idType, ", ", p.MessageTypeName(m.Request), ">();")
		p.P("const out: WSCallOut<", idType, ", ", p.MessageTypeName(m.Response), ", ", p.MessageTypeName(m.Request), "> = {")
		p.P(sendDrained)
		p.P("signal: closed.signal,")
		p.P("close: (code = 1000, reason = \"\") => { ", socketVar, ".close(code, wsCloseReason(reason)); },")
		// A socket that's closing or closed silently drops sends, so without
		// this a call() made after the peer left would never settle.
		p.P("call: (id, value, options = {}) => {")
		p.P("if (", socketVar, ".readyState !== 1) pending.rejectAll(new WSClosedError());")
		p.P("if (options.signal?.aborted) return Promise.reject(wsAbortError(options.signal));")
		if onkir.MessageHasWSTimeout(m.Response) {
			p.P("if (options.timeoutMs !== undefined) value = ", p.timeoutFnName(m.Response), "(value, options.timeoutMs);")
		}
		if cancelFrame, ok := tsWSCancelFrame(p, m.Response, "id"); ok {
			p.P("const reply = pending.register(id, ", tsWSVariantExpression("value", m.Response), ", options, () => { if (", socketVar, ".readyState === 1) ", socketVar, ".send(", cancelFrame, "); });")
		} else {
			p.P("const reply = pending.register(id, ", tsWSVariantExpression("value", m.Response), ", options);")
		}
		p.P("if (!pending.closed) ", sendFrame, ";")
		p.P("return reply;")
		p.P("},")
		p.P("};")
		p.P(socketVar, `.addEventListener("close", (event: any) => {`)
		p.P("const err = new WSClosedError(event?.code, event?.reason);")
		p.P("pending.rejectAll(err);")
		p.P("closed.abort(err);")
		p.P("});")
	} else {
		p.P("const out: WSOut<", p.MessageTypeName(m.Response), "> = {")
		p.P(sendDrained)
		p.P("signal: closed.signal,")
		p.P("close: (code = 1000, reason = \"\") => { ", socketVar, ".close(code, wsCloseReason(reason)); },")
		p.P("};")
		p.P(socketVar, `.addEventListener("close", (event: any) => { closed.abort(new WSClosedError(event?.code, event?.reason)); });`)
	}
	p.P("const runHandler = async (frame: ", p.MessageTypeName(m.Request), "): Promise<void> => {")
	p.P("try {")
	p.P("await handler.", CamelCase(m.Name), "(frame, out);")
	p.P("} catch (err) {")
	p.P(socketVar, ".close(1011, wsCloseReason(err instanceof Error ? err.message : String(err)));")
	p.P("}")
	p.P("};")
	p.P(socketVar, ".addEventListener(\"message\", async (event: any) => {")
	p.P("if (wsFrameTooLarge(event.data, maxFrameBytes)) {")
	p.P(socketVar, `.close(1009, "message too big");`)
	p.P("return;")
	p.P("}")
	// Failures end the connection with a close code and the message as the
	// reason, never an off-schema frame: 1007 for a frame that does not
	// decode or validate, 1011 for a handler error - the same as Go and Rust.
	p.P("let payload: string | Uint8Array | undefined;")
	p.P("try {")
	p.P("payload = assembler.feed(event.data);")
	p.P("} catch (err) {")
	p.P(socketVar, `.close(err instanceof WSChunkError ? err.code : 1007, "invalid chunked message");`)
	p.P("return;")
	p.P("}")
	p.P("if (payload === undefined) return;")
	p.P("let frame: ", p.MessageTypeName(m.Request), ";")
	p.P("try {")
	p.P("frame = wsDecodeMessage(payload, ", p.MessageCodecName(m.Request, "decode"), ", ", reqJoin, ");")
	p.P("} catch {")
	p.P(socketVar, `.close(1007, "invalid JSON frame");`)
	p.P("return;")
	p.P("}")
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(frame);")
	p.P("if (violations.length > 0) {")
	p.P(socketVar, `.close(1007, wsCloseReason(violations.join("; ")));`)
	p.P("return;")
	p.P("}")
	if correlated {
		p.P("const replyId = ", tsWSIDExpression(p, "frame", m.Request, idField), ";")
		p.P("if (replyId !== undefined && pending.resolve(replyId, ", tsWSVariantExpression("frame", m.Request), ", frame)) return;")
	}
	p.P("await runHandler(frame);")
	p.P("});")
	p.P("void runHandler(connection);")
}

func writeTSSocketFactory(p *Printer, s *onkir.Service) {
	factory := "create" + s.Name + "SocketRoutes"
	p.P("export function ", factory, "(handler: ", s.Name, "Handler, options: WSServerOptions = {}): SocketRouteDescriptor[] {")
	p.P("const maxFrameBytes = options.maxFrameBytes ?? DEFAULT_MAX_WS_FRAME_BYTES;")
	p.P("const maxMessageBytes = options.maxMessageBytes ?? DEFAULT_MAX_WS_MESSAGE_BYTES;")
	p.P("const highWaterMark = options.highWaterMarkBytes ?? DEFAULT_WS_HIGH_WATER_MARK_BYTES;")
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
	p.P("export function ", factory, "(httpServer: HttpServer, handler: ", s.Name, "Handler, options: WSServerOptions = {}): void {")
	p.P("const maxFrameBytes = options.maxFrameBytes ?? DEFAULT_MAX_WS_FRAME_BYTES;")
	p.P("const maxMessageBytes = options.maxMessageBytes ?? DEFAULT_MAX_WS_MESSAGE_BYTES;")
	p.P("const highWaterMark = options.highWaterMarkBytes ?? DEFAULT_WS_HIGH_WATER_MARK_BYTES;")
	p.P("// ws enforces the cap itself (closing with 1009); 0 means unlimited there.")
	p.P("const wss = new WebSocketServer({ noServer: true, maxPayload: maxFrameBytes < 0 ? 0 : maxFrameBytes });")
	p.P("const pingInterval = options.pingIntervalMs ?? 30000;")
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
				p.P("body.", field.Name, " = parseScalar(match[", fmt.Sprintf("%q", paramName), "] ?? \"\", ", fmt.Sprintf("%q", field.Type.Scalar.String()), ", ", fmt.Sprintf("%q", "path parameter "+paramName), ");")
			} else {
				p.P("body.", field.Name, " = match[", fmt.Sprintf("%q", paramName), "] ?? \"\";")
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
	p.P("// ws reports protocol violations (an oversized or malformed frame) as")
	p.P("// an 'error' event before closing; unhandled, that would crash the process.")
	p.P(`ws.on("error", () => {});`)
	p.P("if (pingInterval > 0) {")
	p.P("let alive = true;")
	p.P(`ws.on("pong", () => { alive = true; });`)
	p.P(`ws.on("message", () => { alive = true; });`)
	p.P("const timer = setInterval(() => {")
	p.P("if (!alive) { ws.terminate(); return; }")
	p.P("alive = false;")
	p.P("ws.ping();")
	p.P("}, pingInterval);")
	p.P(`ws.on("close", () => clearInterval(timer));`)
	p.P("}")
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
		p.P("private pending = new WSPending<", p.TSFieldType(idField.Type), ", ", resRef, ">();")
	}
	p.P("private inboxQueue: ", resRef, "[] = [];")
	p.P("private inboxWaiters: Array<{ resolve: (value: ", resRef, ") => void; reject: (err: unknown) => void }> = [];")
	p.P("private closedWith: unknown = undefined;")
	p.P("private localClose: WSClosedError | undefined = undefined;")
	p.P("private assembler: WSAssembler;")
	p.P("constructor(private ws: WebSocket, private maxFrameBytes: number = DEFAULT_MAX_WS_FRAME_BYTES, private highWaterMark: number = DEFAULT_WS_HIGH_WATER_MARK_BYTES, maxMessageBytes: number = DEFAULT_MAX_WS_MESSAGE_BYTES) {")
	p.P("this.assembler = new WSAssembler(maxMessageBytes);")
	p.P("this.listen();")
	p.P("}")
	p.P()
	writeTSDuplexSend(p, m, reqRef)
	p.P("private failLocally(code: number, reason: string): void {")
	p.P("this.localClose = new WSClosedError(code, reason);")
	p.P("this.ws.close(1000, reason);")
	p.P("}")
	p.P()
	writeTSDuplexListen(p, m, resRef, correlated)
	p.P("receive(): Promise<", resRef, "> {")
	p.P("const queued = this.inboxQueue.shift();")
	p.P("if (queued !== undefined) return Promise.resolve(queued);")
	p.P("if (this.closedWith !== undefined) return Promise.reject(this.closedWith);")
	p.P("return new Promise((resolve, reject) => { this.inboxWaiters.push({ resolve, reject }); });")
	p.P("}")
	p.P()
	if correlated {
		writeTSDuplexCall(p, m, reqRef, resRef)
	}
	p.P("close(): void { this.ws.close(); }")
	p.P("}")
	p.P()
}

func writeTSDuplexListen(p *Printer, m *onkir.Method, resRef string, correlated bool) {
	p.P("private listen(): void {")
	p.P("this.ws.addEventListener(\"message\", (event: MessageEvent) => {")
	p.P("if (wsFrameTooLarge(event.data, this.maxFrameBytes)) {")
	p.P(`this.failLocally(1009, "message too big");`)
	p.P("return;")
	p.P("}")
	p.P("let payload: string | Uint8Array | undefined;")
	p.P("try {")
	p.P("payload = this.assembler.feed(event.data);")
	p.P("} catch (err) {")
	p.P(`this.failLocally(err instanceof WSChunkError ? err.code : 1007, "invalid chunked message");`)
	p.P("return;")
	p.P("}")
	p.P("if (payload === undefined) return;")
	p.P("let frame: ", resRef, ";")
	p.P("try {")
	_, respJoin := p.wsRawCodecArgs(m.Response)
	p.P("frame = wsDecodeMessage(payload, ", p.MessageCodecName(m.Response, "decode"), ", ", respJoin, ");")
	p.P("} catch {")
	p.P(`this.failLocally(1007, "invalid frame");`)
	p.P("return;")
	p.P("}")
	if correlated {
		idField, _ := m.WSIDField()
		p.P("const replyId = ", tsWSIDExpression(p, "frame", m.Response, idField), ";")
		p.P("if (replyId !== undefined && this.pending.resolve(replyId, ", tsWSVariantExpression("frame", m.Response), ", frame)) return;")
	}
	p.P("const waiter = this.inboxWaiters.shift();")
	p.P("if (waiter) { waiter.resolve(frame); return; }")
	p.P("this.inboxQueue.push(frame);")
	p.P("});")
	p.P(`this.ws.addEventListener("close", (event: CloseEvent) => {`)
	p.P("this.closedWith = this.localClose ?? new WSClosedError(event.code, event.reason);")
	if correlated {
		p.P("this.pending.rejectAll(this.closedWith);")
	}
	p.P("for (const waiter of this.inboxWaiters.splice(0)) waiter.reject(this.closedWith);")
	p.P("});")
	p.P("}")
	p.P()
}

func writeTSDuplexCall(p *Printer, m *onkir.Method, reqRef, resRef string) {
	idField, _ := m.WSIDField()
	p.P("call(id: ", p.TSFieldType(idField.Type), ", value: ", reqRef, ", options: WSCallOptions = {}): Promise<", resRef, "> {")
	p.P("if (this.ws.readyState !== 1) this.pending.rejectAll(this.closedWith ?? new WSClosedError());")
	p.P("if (options.signal?.aborted) return Promise.reject(wsAbortError(options.signal));")
	if onkir.MessageHasWSTimeout(m.Request) {
		p.P("if (options.timeoutMs !== undefined) value = ", p.timeoutFnName(m.Request), "(value, options.timeoutMs);")
	}
	if cancelFrame, ok := tsWSCancelFrame(p, m.Request, "id"); ok {
		p.P("const reply = this.pending.register(id, ", tsWSVariantExpression("value", m.Request), ", options, () => { if (this.ws.readyState === 1) this.ws.send(", cancelFrame, "); });")
	} else {
		p.P("const reply = this.pending.register(id, ", tsWSVariantExpression("value", m.Request), ", options);")
	}
	p.P("if (this.pending.closed) return reply;")
	p.P("try {")
	p.P("this.send(value);")
	p.P("} catch (err) {")
	p.P("this.pending.fail(id, err);")
	p.P("}")
	p.P("return reply;")
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
	p.P(`if (violations.length > 0) throw new RequestValidationError("invalid request", violations);`)
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
	p.P(`ws.binaryType = "arraybuffer";`)
	p.P(`ws.onopen = () => resolve(new `, tsDuplexName(m), "(ws, this.options.maxFrameBytes ?? DEFAULT_MAX_WS_FRAME_BYTES, this.options.highWaterMarkBytes ?? DEFAULT_WS_HIGH_WATER_MARK_BYTES, this.options.maxMessageBytes ?? DEFAULT_MAX_WS_MESSAGE_BYTES));")
	p.P(`ws.onerror = () => reject(new Error("websocket connection failed"));`)
	p.P("});")
	p.P("}")
	p.P()
}

// writeTSDuplexSend emits a duplex class's send(), split out of
// writeTSDuplexClass for the linter's statement-count limit.
func writeTSDuplexSend(p *Printer, m *onkir.Method, reqRef string) {
	p.P("// send throws on an invalid frame, and otherwise resolves once the")
	p.P("// socket's buffer is back under the high-water mark (backpressure).")
	p.P("send(value: ", reqRef, "): Promise<void> {")
	reqSplit, _ := p.wsRawCodecArgs(m.Request)
	p.P("const violations = ", p.MessageCodecName(m.Request, "validate"), "(value);")
	p.P(`if (violations.length > 0) throw new RequestValidationError("invalid frame", violations);`)
	p.P("wsSend(this.ws, wsEncodeMessage(value, ", p.MessageCodecName(m.Request, "encode"), ", ", reqSplit, "));")
	p.P("return wsDrained(this.ws, this.highWaterMark);")
	p.P("}")
	p.P()
}
