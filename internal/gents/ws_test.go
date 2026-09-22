package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wsFixture = `
package wsf

message ChatMessage { room: string text: string }
message ChatEvent { seq: int64 }

service ChatService {
  base_path: "/v1"

  chat(ChatMessage) -> ChatEvent @ws("/rooms/{room}")
}
`

func TestGenerateTSWSServer(t *testing.T) {
	file, err := compileForTest(wsFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateServerWithResolver(file, nil))
	for _, want := range []string{
		`export interface WSOut<E> {`,
		"export interface SocketRouteDescriptor {",
		"chat(req: ChatMessage, out: WSOut<ChatEvent>): void | Promise<void>;",
		`export function createChatServiceSocketRoutes(handler: ChatServiceHandler, options: WSServerOptions = {}): SocketRouteDescriptor[] {`,
		`upgrade") || "").toLowerCase() !== "websocket"`,
		"(globalThis as any).WebSocketPair()",
		// Node adapter: no built-in WebSocketPair, so it's an independent
		// path built on the `ws` package instead, sharing the same out/
		// handler-dispatch body as the Workers-style route above.
		`import { WebSocketServer } from "ws";`,
		`import type { Server as HttpServer, IncomingHttpHeaders, IncomingMessage } from "node:http";`,
		"export function attachChatServiceNodeSocketHandlers(httpServer: HttpServer, handler: ChatServiceHandler, options: WSServerOptions = {}): void {",
		`const match = matchPath("/v1/rooms/{room}", url.pathname);`,
		"wss.handleUpgrade(req, socket, head, (ws) => {",
		// Outgoing frames are encoded to the wire shape on both paths, like
		// the TS client's send(); JSON.stringify(value) alone leaks camelCase.
		"send: (value) => { server.send(JSON.stringify(encodeChatEvent(value))); return wsDrained(server, highWaterMark); },",
		"send: (value) => { ws.send(JSON.stringify(encodeChatEvent(value))); return wsDrained(ws, highWaterMark); },",
		// One shared 'upgrade' listener per http.Server rejects paths no
		// route claims instead of leaking the socket until TCP timeout.
		`const nodeSocketRoutesKey = Symbol.for("onekit.nodeSocketRoutes");`,
		"registerNodeSocketRoute(httpServer, (req, socket, head, url) => {",
		`socket.write("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n");`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated ts server missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateTSWSClient(t *testing.T) {
	file, err := compileForTest(wsFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateClientWithResolver(file, nil))
	for _, want := range []string{
		"export class ChatSocket {",
		"async chat(req: ChatMessage): Promise<ChatSocket> {",
		`.replace(/^https:/, "wss:").replace(/^http:/, "ws:")`,
		"new WebSocket(socketURL)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated ts client missing %q:\n%s", want, text)
		}
	}
}

const wsCorrelatedFixture = `
package wsc

message RunRequest { code: string }
message RunResult { exit_code: int32 }
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }
message Cancel { id: string @ws_id }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
    cancel: Cancel @tag("cancel") @ws_cancel
  }
}

service Runtime {
  base_path: "/v1"

  execute(Frame) -> Frame @ws("/execute")
}
`

func TestGenerateTSWSServerCorrelated(t *testing.T) {
	file, err := compileForTest(wsCorrelatedFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateServerWithResolver(file, nil))
	for _, want := range []string{
		"export class WSPending<K, T> {",
		"export interface WSCallOut<K, E, R> extends WSOut<E> {",
		"execute(req: Frame, out: WSCallOut<string, Frame, Frame>): void | Promise<void>;",
		"const pending = new WSPending<string, Frame>();",
		"if (server.readyState !== 1) pending.rejectAll(new WSClosedError());",
		"if (!pending.closed) server.send(JSON.stringify(encodeFrame(value)));",
		"if (replyId !== undefined && pending.resolve(replyId, ((): string => {",
		// Both oneof variants carrying @ws_id must get extraction code, not
		// just whichever one happens to be first by declaration order.
		`if (frame.payload && frame.payload.type === "host_call") return frame.payload.hostCall.id;`,
		`if (frame.payload && frame.payload.type === "host_result") return frame.payload.hostResult.id;`,
		// Node adapter gets the same correlated out/call shape, reusing the
		// identical shared body (just socketVar "ws" instead of "server").
		"export function attachRuntimeNodeSocketHandlers(httpServer: HttpServer, handler: RuntimeHandler, options: WSServerOptions = {}): void {",
		"if (!pending.closed) ws.send(JSON.stringify(encodeFrame(value)));",
		"if (this.isClosed) return Promise.reject(this.closedWith);",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated correlated ts server missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateTSWSClientCorrelated(t *testing.T) {
	file, err := compileForTest(wsCorrelatedFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateClientWithResolver(file, nil))
	for _, want := range []string{
		"export class WSPending<K, T> {",
		"export class ExecuteSocket {",
		"private pending = new WSPending<string, Frame>();",
		"call(id: string, value: Frame, options: WSCallOptions = {}): Promise<Frame> {",
		"private ensureListening(): void {",
		`if (frame.payload && frame.payload.type === "host_call") return frame.payload.hostCall.id;`,
		`if (frame.payload && frame.payload.type === "host_result") return frame.payload.hostResult.id;`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated correlated ts client missing %q:\n%s", want, text)
		}
	}
}

// TestGeneratedTSWSTypeChecks covers the plain (no @ws_id) fixture. There
// was previously no tsc check for @ws server output at all - the general
// TestGeneratedTypeScriptTypeChecks fixture has no @ws methods - so this
// gap (and the correlated one below) existed independent of the Node
// adapter work; closing both while touching this file for it.
func TestGeneratedTSWSTypeChecks(t *testing.T) {
	runTSWSTypeCheck(t, wsFixture, "ws-typecheck")
}

func TestGeneratedTSWSCorrelatedTypeChecks(t *testing.T) {
	runTSWSTypeCheck(t, wsCorrelatedFixture, "ws-correlated-typecheck")
}

// runTSWSTypeCheck generates types+client+server for fixture and type-checks
// them together with a real tsc. server.ts always includes the Node adapter
// (attach...NodeSocketHandlers) now, which needs the `ws` package and
// @types/node for `node:http` - the same peer dependencies a real consuming
// project would add. ws's own bundled types don't resolve cleanly under
// "moduleResolution": "bundler" (its ESM wrapper entrypoint isn't typed), so
// @types/ws too.
func runTSWSTypeCheck(t *testing.T, fixture, packageName string) {
	t.Helper()
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	file, err := compileForTest(fixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClientWithResolver(file, nil)
	serverSrc := GenerateServerWithResolver(file, nil)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "client.ts"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "server.ts"), string(serverSrc))
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "`+packageName+`", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "types": ["node", "ws"],
    "lib": ["ES2022", "DOM"]
  }
}
`)

	install := exec.Command("npm", "install", "--no-audit", "--no-fund", "ws", "@types/node", "@types/ws")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, out)
	}

	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc type check failed: %v\n%s", err, out)
	}
}

// tsNodeRuntimeHarness drives the generated attach...NodeSocketHandlers
// against a real Node http.Server and a real `ws` client through the same
// multi-variant correlated round trip as the Go runtime test
// (TestGeneratedWSCorrelatedRuntimeRoutesMultipleVariants in gengo): the
// server answers the client's "run" frame by pushing a "host_call" and
// awaiting the matching "host_result" (a *different* oneof variant) before
// sending "run_result". This is plain JS, not compiled from TS, since it's
// test-only harness code exercising the compiled generated output rather
// than something onekit ships - the generated server.ts/types.ts are what
// get tsc-compiled and are the actual subject under test.
const tsNodeRuntimeHarness = `
"use strict";
const http = require("node:http");
const WebSocket = require("ws");
const { attachRuntimeNodeSocketHandlers } = require("./server.js");

// out.call()/out.send() take the typed TS Frame shape (camelCase variant
// keys) and encode it to the wire, the same as the generated TS client's
// send(); the raw client side of this harness speaks wire JSON (snake_case)
// directly, so a server that forgot to encode would be caught here too.
const handler = {
  async execute(req, out) {
    if (!req.payload || req.payload.type !== "run") return;
    try {
      const reply = await out.call("call-1", { payload: { type: "host_call", hostCall: { id: "call-1", method: "doThing" } } });
      const result = reply.payload && reply.payload.type === "host_result" ? reply.payload.hostResult : null;
      if (!result || result.id !== "call-1" || result.value !== "answer") {
        console.error("UNEXPECTED_REPLY", JSON.stringify(reply));
        process.exit(1);
      }
      await out.send({ payload: { type: "run_result", runResult: { exitCode: 0 } } });
    } catch (err) {
      console.error("CALL_ERROR", err);
      process.exit(1);
    }
  },
};

const httpServer = http.createServer((req, res) => { res.writeHead(404); res.end(); });
attachRuntimeNodeSocketHandlers(httpServer, handler);

httpServer.listen(0, "127.0.0.1", () => {
  const port = httpServer.address().port;
  const ws = new WebSocket("ws://127.0.0.1:" + port + "/v1/execute");
  const timer = setTimeout(() => {
    console.error("TIMEOUT - Call() never got its host_result reply");
    process.exit(1);
  }, 10000);

  ws.on("open", () => {
    ws.send(JSON.stringify({ payload: { type: "run", run: { code: "print(1)" } } }));
  });
  ws.on("message", (data) => {
    const frame = JSON.parse(String(data));
    if (frame.payload && frame.payload.type === "host_call") {
      if (!frame.payload.host_call || frame.payload.host_call.method !== "doThing") {
        console.error("SERVER_SENT_UNENCODED_FRAME", JSON.stringify(frame));
        process.exit(1);
      }
      ws.send(JSON.stringify({ payload: { type: "host_result", host_result: { id: frame.payload.host_call.id, value: "answer" } } }));
      return;
    }
    if (frame.payload && frame.payload.type === "run_result") {
      if (!frame.payload.run_result || frame.payload.run_result.exit_code !== 0) {
        console.error("SERVER_SENT_UNENCODED_FRAME", JSON.stringify(frame));
        process.exit(1);
      }
      clearTimeout(timer);
      console.log("OK");
      process.exit(0);
    }
  });
  ws.on("error", (err) => {
    console.error("WS_ERROR", err);
    process.exit(1);
  });
});
`

func TestGeneratedTSWSNodeAdapterRuntimeRoutesMultipleVariants(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture, nil)
	runNodeHarness(t, dir, tsNodeRuntimeHarness)
}

// tsNodeLifecycleHarness pins two connection-lifecycle bugs in the Node
// adapter: an upgrade to a path no route claims used to be left hanging until
// TCP timeout (no listener ever destroyed the socket), and a call() made after
// the peer closed used to register a waiter and "send" into a dead socket,
// never settling.
const tsNodeLifecycleHarness = `
"use strict";
const http = require("node:http");
const WebSocket = require("ws");
const { attachRuntimeNodeSocketHandlers } = require("./server.js");

function fail(message) { console.error(message); process.exit(1); }
const timer = setTimeout(() => fail("TIMEOUT"), 10000);

let settleCall;
const callOutcome = new Promise((resolve) => { settleCall = resolve; });

const handler = {
  async execute(req, out) {
    if (!req.payload || req.payload.type !== "run") return;
    // Let the peer's close land first - the reported repro.
    await new Promise((resolve) => setTimeout(resolve, 300));
    try {
      await out.call("late-1", { payload: { type: "host_call", hostCall: { id: "late-1", method: "late" } } });
      settleCall("RESOLVED");
    } catch {
      settleCall("REJECTED");
    }
  },
};

const httpServer = http.createServer((req, res) => { res.writeHead(404); res.end(); });
attachRuntimeNodeSocketHandlers(httpServer, handler);

httpServer.listen(0, "127.0.0.1", async () => {
  const base = "ws://127.0.0.1:" + httpServer.address().port;

  const unmatched = await new Promise((resolve) => {
    const ws = new WebSocket(base + "/v1/nope");
    ws.on("unexpected-response", (_req, res) => resolve("status " + res.statusCode));
    ws.on("open", () => resolve("opened"));
    ws.on("error", (err) => resolve("error " + err.message));
  });
  if (unmatched !== "status 404") fail("UNMATCHED_PATH: " + unmatched);

  const ws = new WebSocket(base + "/v1/execute");
  ws.on("open", () => {
    ws.send(JSON.stringify({ payload: { type: "run", run: { code: "x" } } }));
    setTimeout(() => ws.close(), 50);
  });
  const outcome = await callOutcome;
  if (outcome !== "REJECTED") fail("LATE_CALL: " + outcome);

  clearTimeout(timer);
  console.log("OK");
  process.exit(0);
});
`

// tsNodeCancelHarness covers call() bounds end to end: a server call()
// with timeoutMs rejects with WSTimeoutError and sends the peer the schema's
// @ws_cancel frame; an aborted signal does the same with WSCancelledError; the
// generated TS client's call() does both in the other direction; and a frame
// over maxFrameBytes closes the connection with 1009.
const tsNodeCancelHarness = `
"use strict";
const http = require("node:http");
const WebSocket = require("ws");
const server = require("./server.js");
const client = require("./client.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT"), 15000).unref();

const hostCall = (id) => ({ payload: { type: "host_call", hostCall: { id, method: "slow" } } });
const serverFrames = [];
let serverFramesChanged = () => {};

const handler = {
  async execute(req, out) {
    // The connect-time call carries the (empty) upgrade request.
    if (!req.payload) return;
    if (req.payload.type !== "run") {
      serverFrames.push(req);
      serverFramesChanged();
      return;
    }
    if (req.payload.run.code !== "server-calls") return;
    try {
      await out.call("slow-1", hostCall("slow-1"), { timeoutMs: 150 });
      fail("timeoutMs call resolved");
    } catch (err) {
      if (!(err instanceof server.WSTimeoutError)) fail("want WSTimeoutError, got", err);
    }
    const controller = new AbortController();
    setTimeout(() => controller.abort(), 50);
    try {
      await out.call("ab-1", hostCall("ab-1"), { signal: controller.signal });
      fail("aborted call resolved");
    } catch (err) {
      if (!(err instanceof server.WSCancelledError)) fail("want WSCancelledError, got", err);
    }
    out.send({ payload: { type: "run_result", runResult: { exitCode: 7 } } });
  },
};

function listen(options) {
  const httpServer = http.createServer((req, res) => { res.writeHead(404); res.end(); });
  server.attachRuntimeNodeSocketHandlers(httpServer, handler, options);
  return new Promise((resolve) => httpServer.listen(0, "127.0.0.1", () => resolve("127.0.0.1:" + httpServer.address().port)));
}

function serverCallsCancel(addr) {
  return new Promise((resolve) => {
    const ws = new WebSocket("ws://" + addr + "/v1/execute");
    const seen = [];
    ws.on("open", () => ws.send(JSON.stringify({ payload: { type: "run", run: { code: "server-calls" } } })));
    ws.on("message", (data) => {
      const p = JSON.parse(String(data)).payload;
      seen.push(p.type + ":" + (p.host_call || p.cancel || {}).id);
      if (p.type === "run_result") { ws.close(); resolve(seen.join(",")); }
    });
  });
}

async function clientCallCancels(addr) {
  const socket = await new client.RuntimeClient("http://" + addr).execute({ payload: { type: "run", run: { code: "idle" } } });
  try {
    await socket.call("c-1", hostCall("c-1"), { timeoutMs: 100 });
    fail("client timeoutMs call resolved");
  } catch (err) {
    if (!(err instanceof client.WSTimeoutError)) fail("want client WSTimeoutError, got", err);
  }
  await new Promise((resolve) => {
    serverFramesChanged = () => { if (serverFrames.length >= 2) resolve(); };
    serverFramesChanged();
  });
  socket.close();
  return serverFrames.map((f) => f.payload.type + ":" + (f.payload.hostCall || f.payload.cancel).id).join(",");
}

function oversizedFrameCloseCode(addr) {
  return new Promise((resolve) => {
    const ws = new WebSocket("ws://" + addr + "/v1/execute");
    ws.on("open", () => ws.send(JSON.stringify({ payload: { type: "run", run: { code: "x".repeat(4096) } } })));
    ws.on("close", (code) => resolve(code));
  });
}

(async () => {
  const addr = await listen();
  const seen = await serverCallsCancel(addr);
  if (seen !== "host_call:slow-1,cancel:slow-1,host_call:ab-1,cancel:ab-1,run_result:undefined") fail("server-side frames:", seen);

  const handled = await clientCallCancels(addr);
  if (handled !== "host_call:c-1,cancel:c-1") fail("client-side frames:", handled);

  const small = await listen({ maxFrameBytes: 1024 });
  const code = await oversizedFrameCloseCode(small);
  if (code !== 1009) fail("oversized frame close code:", code);

  console.log("OK");
  process.exit(0);
})().catch((err) => fail("HARNESS_ERROR", err));
`

// Also pins that an oversized frame closes only that connection: the Node
// adapter used to leave ws's 'error' event unhandled, crashing the process.
func TestGeneratedTSWSNodeCallCancellationAndLimits(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture, nil)
	runNodeHarness(t, dir, tsNodeCancelHarness)
}

// tsNodeProtocolHarness pins three wire behaviors of the Node adapter:
//   - an inbound host_call reusing the id of the server's own in-flight
//     host_call is a new call for the handler, not that call's reply;
//   - a frame that fails to decode closes with 1007, a handler error with
//     1011 and its message as the reason, and neither sends an off-schema
//     {"error": ...} frame first;
//   - out.send() applies backpressure: with the client paused, awaited sends
//     stall at the high-water mark instead of queueing without bound.
const tsNodeProtocolHarness = `
"use strict";
const http = require("node:http");
const WebSocket = require("ws");
const server = require("./server.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT"), 20000).unref();

const hostCall = (id, method) => ({ payload: { type: "host_call", hostCall: { id, method } } });
let handlerSaw = [];
let callSettled = null;
let sent = 0;

const handler = {
  async execute(req, out) {
    if (!req.payload) return;
    const p = req.payload;
    if (p.type === "run" && p.run.code === "collide") {
      callSettled = out.call("dup-1", hostCall("dup-1", "server")).then((r) => r.payload.type, (e) => "rejected " + e);
      return;
    }
    if (p.type === "run" && p.run.code === "boom") throw new Error("boom");
    if (p.type === "run" && p.run.code === "flood") {
      const chunk = "x".repeat(1024 * 1024);
      for (let i = 0; i < 64; i++) { await out.send(hostCall("f" + i, chunk)); sent++; }
      return;
    }
    handlerSaw.push(p.type + ":" + (p.hostCall ? p.hostCall.method : ""));
  },
};

const httpServer = http.createServer((req, res) => { res.writeHead(404); res.end(); });
server.attachRuntimeNodeSocketHandlers(httpServer, handler, { highWaterMarkBytes: 1024 * 1024, maxFrameBytes: 128 * 1024 * 1024 });

function connect(addr) {
  return new Promise((resolve) => {
    const ws = new WebSocket("ws://" + addr + "/v1/execute");
    const frames = [];
    ws.on("message", (data) => frames.push(JSON.parse(String(data))));
    ws.on("open", () => resolve({ ws, frames }));
  });
}

function closeOf(ws) {
  return new Promise((resolve) => ws.on("close", (code, reason) => resolve(code + " " + String(reason))));
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

httpServer.listen(0, "127.0.0.1", async () => {
  const addr = "127.0.0.1:" + httpServer.address().port;

  // Colliding id.
  const a = await connect(addr);
  a.ws.send(JSON.stringify({ payload: { type: "run", run: { code: "collide" } } }));
  await sleep(100);
  a.ws.send(JSON.stringify({ payload: { type: "host_call", host_call: { id: "dup-1", method: "client" } } }));
  await sleep(100);
  if (handlerSaw.join(",") !== "host_call:client") fail("colliding call did not reach the handler:", handlerSaw, await callSettled);
  a.ws.send(JSON.stringify({ payload: { type: "host_result", host_result: { id: "dup-1", value: "ok" } } }));
  const settled = await callSettled;
  if (settled !== "host_result") fail("server call settled with", settled);
  a.ws.close();

  // Close codes, and nothing but the close.
  const b = await connect(addr);
  const bClosed = closeOf(b.ws);
  b.ws.send("not json");
  const bClose = await bClosed;
  if (bClose !== "1007 invalid JSON frame" || b.frames.length !== 0) fail("invalid frame:", bClose, JSON.stringify(b.frames));

  const c = await connect(addr);
  const cClosed = closeOf(c.ws);
  c.ws.send(JSON.stringify({ payload: { type: "run", run: { code: "boom" } } }));
  const cClose = await cClosed;
  if (cClose !== "1011 boom" || c.frames.length !== 0) fail("handler error:", cClose, JSON.stringify(c.frames));

  // Backpressure.
  const d = await connect(addr);
  d.ws.pause();
  d.ws.send(JSON.stringify({ payload: { type: "run", run: { code: "flood" } } }));
  await sleep(1000);
  if (sent >= 64) fail("out.send never waited: all 64 MiB sent to a paused client");
  const stalledAt = sent;
  d.ws.resume();
  for (let i = 0; i < 200 && sent < 64; i++) await sleep(50);
  if (sent < 64) fail("sends never resumed after the client drained:", stalledAt, "->", sent);
  d.ws.close();

  console.log("OK");
  process.exit(0);
});
`

func TestGeneratedTSWSNodeReplyDirectionErrorsBackpressure(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture, nil)
	runNodeHarness(t, dir, tsNodeProtocolHarness)
}

func TestGeneratedTSWSNodeAdapterLifecycle(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture, nil)
	runNodeHarness(t, dir, tsNodeLifecycleHarness)
}

// buildTSNodeServer generates types.ts/server.ts/client.ts for fixture, plus any extra files, into a temp dir,
// installs the Node adapter's peer dependencies, and compiles them to
// CommonJS so a plain-JS harness can require("./server.js").
func buildTSNodeServer(t *testing.T, fixture string, extra map[string]string) string {
	t.Helper()
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	file, err := compileForTest(fixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	dir := t.TempDir()
	for name, content := range extra {
		writeFile(t, filepath.Join(dir, name), content)
	}
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServerWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClientWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "ws-node-runtime", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "node16",
    "moduleResolution": "node16",
    "esModuleInterop": true,
    "strict": true,
    "types": ["node", "ws"],
    "lib": ["ES2022", "DOM"]
  }
}
`)
	install := exec.Command("npm", "install", "--no-audit", "--no-fund", "ws", "@types/node", "@types/ws")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, out)
	}
	build := exec.Command("tsc", "-p", "tsconfig.json")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("tsc build failed: %v\n%s", err, out)
	}
	return dir
}

func runNodeHarness(t *testing.T, dir, harness string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "harness.js"), harness)
	run := exec.Command("node", "harness.js")
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "OK" {
		t.Fatalf("expected OK, got %q", got)
	}
}

const nodeHTTPFixture = `
package nodehttp

message GetReq { id: string }
message Item { id: string
name: string }
message CreateReq { name: string }

service Items {
  base_path: "/v1"

  get(GetReq) -> Item @get("/items/{id}")
  create(CreateReq) -> Item @post("/items")
}
`

const nodeHTTPTypeCheck = `
import * as http from "node:http";
import { attachItemsNodeHandlers, createItemsNodeHandler, type ItemsHandler } from "./server.js";

declare const handler: ItemsHandler;
attachItemsNodeHandlers(http.createServer(), handler);
const api = createItemsNodeHandler(handler);
http.createServer((req, res) => { if (!api(req, res)) { res.statusCode = 404; res.end(); } });
`

const tsNodeHTTPHarness = `
"use strict";
const http = require("node:http");
const { attachItemsNodeHandlers } = require("./server.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT"), 10000).unref();

const handler = {
  async get(req) { return { id: req.id, name: "stored" }; },
  async create(req) { return { id: "new", name: req.name }; },
};

const server = http.createServer();
attachItemsNodeHandlers(server, handler);
server.listen(0, "127.0.0.1", async () => {
  const base = "http://127.0.0.1:" + server.address().port;
  const got = await fetch(base + "/v1/items/abc");
  const gotBody = await got.json();
  if (got.status !== 200 || gotBody.id !== "abc" || gotBody.name !== "stored") fail("GET:", got.status, JSON.stringify(gotBody));
  const created = await fetch(base + "/v1/items", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: "widget" }) });
  const createdBody = await created.json();
  if (created.status !== 200 || createdBody.name !== "widget") fail("POST:", created.status, JSON.stringify(createdBody));
  const missing = await fetch(base + "/v1/nope");
  if (missing.status !== 404) fail("unmatched path:", missing.status);
  console.log("OK");
  process.exit(0);
});
`

func TestGeneratedTSNodeHTTPAdapter(t *testing.T) {
	dir := buildTSNodeServer(t, nodeHTTPFixture, map[string]string{"typecheck.ts": nodeHTTPTypeCheck})
	runNodeHarness(t, dir, tsNodeHTTPHarness)
}

const tsNodeSignalKeepAliveHarness = `
"use strict";
const http = require("node:http");
const WebSocket = require("ws");
const server = require("./server.js");
const client = require("./client.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT"), 15000).unref();

let aborted = null;
const handler = {
  async execute(req, out) {
    if (!req.payload || req.payload.type !== "run") return;
    out.signal.addEventListener("abort", () => { aborted = out.signal.reason; });
  },
};

function listen(options) {
  const httpServer = http.createServer();
  server.attachRuntimeNodeSocketHandlers(httpServer, handler, options);
  return new Promise((resolve) => httpServer.listen(0, "127.0.0.1", () => resolve("127.0.0.1:" + httpServer.address().port)));
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const addr = await listen();
  const a = new WebSocket("ws://" + addr + "/v1/execute");
  await new Promise((r) => a.on("open", r));
  a.send(JSON.stringify({ payload: { type: "run", run: { code: "x" } } }));
  await sleep(100);
  a.close(4000, "bye");
  await sleep(200);
  if (!(aborted instanceof server.WSClosedError) || aborted.code !== 4000) fail("out.signal:", aborted);

  const fast = await listen({ pingIntervalMs: 100 });
  const b = new WebSocket("ws://" + fast + "/v1/execute", { autoPong: false });
  const dropped = new Promise((r) => b.on("close", () => r(true)));
  const result = await Promise.race([dropped, sleep(3000).then(() => false)]);
  if (!result) fail("a peer that never answers pings was never dropped");

  const raw = new WebSocket.WebSocketServer({ port: 0, host: "127.0.0.1" });
  await new Promise((r) => raw.on("listening", r));
  raw.on("connection", (ws) => ws.send("not json"));
  const socket = await new client.RuntimeClient("http://127.0.0.1:" + raw.address().port).execute({ payload: { type: "run", run: { code: "x" } } });
  try {
    await socket.call("c-1", { payload: { type: "host_call", hostCall: { id: "c-1", method: "x" } } }, { timeoutMs: 5000 });
    fail("call resolved on an undecodable frame");
  } catch (err) {
    if (!(err instanceof client.WSClosedError) || err.code !== 1007) fail("undecodable frame:", err);
  }
  console.log("OK");
  process.exit(0);
})().catch((err) => fail("HARNESS_ERROR", err));
`

func TestGeneratedTSWSNodeSignalKeepAliveAndDecodeFailure(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture, nil)
	runNodeHarness(t, dir, tsNodeSignalKeepAliveHarness)
}
