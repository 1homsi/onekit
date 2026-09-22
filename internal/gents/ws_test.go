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
		`export function createChatServiceSocketRoutes(handler: ChatServiceHandler): SocketRouteDescriptor[] {`,
		`upgrade") || "").toLowerCase() !== "websocket"`,
		"(globalThis as any).WebSocketPair()",
		// Node adapter: no built-in WebSocketPair, so it's an independent
		// path built on the `ws` package instead, sharing the same out/
		// handler-dispatch body as the Workers-style route above.
		`import { WebSocketServer } from "ws";`,
		`import type { Server as HttpServer, IncomingHttpHeaders, IncomingMessage } from "node:http";`,
		"export function attachChatServiceNodeSocketHandlers(httpServer: HttpServer, handler: ChatServiceHandler): void {",
		`const match = matchPath("/v1/rooms/{room}", url.pathname);`,
		"wss.handleUpgrade(req, socket, head, (ws) => {",
		// Outgoing frames are encoded to the wire shape on both paths, like
		// the TS client's send(); JSON.stringify(value) alone leaks camelCase.
		"send: (value) => { server.send(JSON.stringify(encodeChatEvent(value))); },",
		"send: (value) => { ws.send(JSON.stringify(encodeChatEvent(value))); },",
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

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
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
		`if (server.readyState !== 1) pending.rejectAll(new Error("websocket closed"));`,
		"if (!pending.closed) server.send(JSON.stringify(encodeFrame(value)));",
		"if (replyId !== undefined && pending.resolve(replyId, frame)) return;",
		// Both oneof variants carrying @ws_id must get extraction code, not
		// just whichever one happens to be first by declaration order.
		`if (frame.payload && frame.payload.type === "host_call") return frame.payload.hostCall.id;`,
		`if (frame.payload && frame.payload.type === "host_result") return frame.payload.hostResult.id;`,
		// Node adapter gets the same correlated out/call shape, reusing the
		// identical shared body (just socketVar "ws" instead of "server").
		"export function attachRuntimeNodeSocketHandlers(httpServer: HttpServer, handler: RuntimeHandler): void {",
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
		"call(id: string, value: Frame): Promise<Frame> {",
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
	dir := buildTSNodeServer(t, wsCorrelatedFixture)
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

func TestGeneratedTSWSNodeAdapterLifecycle(t *testing.T) {
	dir := buildTSNodeServer(t, wsCorrelatedFixture)
	runNodeHarness(t, dir, tsNodeLifecycleHarness)
}

// buildTSNodeServer generates types.ts/server.ts for fixture into a temp dir,
// installs the Node adapter's peer dependencies, and compiles them to
// CommonJS so a plain-JS harness can require("./server.js").
func buildTSNodeServer(t *testing.T, fixture string) string {
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
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServerWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "ws-node-runtime", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "node16",
    "moduleResolution": "node16",
    "esModuleInterop": true,
    "strict": true,
    "types": ["node", "ws"]
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
