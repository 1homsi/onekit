package gents

import "testing"

func TestTSNodeAdapterRejectsMalformedHost(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  text: string;
}

interface Notes {
  @post
  @route("/notes")
  create(...Note): Note;
}
`, `
import { createServer } from "node:http";
import { connect } from "node:net";
import { attachNotesNodeHandlers } from "./server.ts";

const server = createServer();
attachNotesNodeHandlers(server, { create: async (req) => req } as any);
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = (server.address() as { port: number }).port;

const raw = (request: string) => new Promise<string>((resolve, reject) => {
  const socket = connect(port, "127.0.0.1", () => socket.write(request));
  let data = "";
  socket.on("data", (chunk) => { data += chunk; });
  socket.on("end", () => resolve(data));
  socket.on("error", reject);
});

const bad = await raw("POST /notes HTTP/1.1\r\nHost: a b\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}");
if (!bad.startsWith("HTTP/1.1 400")) throw new Error("malformed host not rejected: " + bad);
const good = await raw("POST /notes HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 13\r\nConnection: close\r\n\r\n{\"text\":\"hi\"}");
if (!good.startsWith("HTTP/1.1 200")) throw new Error("server died after malformed host: " + good);
server.close();
console.log("OK");
`)
}

func TestTSNodeAdapterRejectsHostWithCredentials(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  text: string;
}

interface Notes {
  @post
  @route("/notes")
  create(...Note): Note;
}
`, `
import { createServer } from "node:http";
import { connect } from "node:net";
import { attachNotesNodeHandlers, createNotesNodeHandler } from "./server.ts";
import type { ServerErrorInfo } from "./server.ts";

const impl = { create: async (req: unknown) => req } as any;
const seen: string[] = [];
const onError = (info: ServerErrorInfo, req: Request) => {
  seen.push(info.code + " " + req.method + " " + req.headers.get("x-request-id"));
  return new Response(JSON.stringify({ error: { code: info.code, message: info.message } }), { status: info.status, headers: { "Content-Type": "application/json" } });
};

const handle = createNotesNodeHandler(impl, { onError });
const custom = createServer((req, res) => { if (!handle(req, res)) { res.statusCode = 404; res.end(); } });
const plain = createServer();
attachNotesNodeHandlers(plain, impl);
const ports = new Map<unknown, number>();
for (const server of [custom, plain]) {
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  ports.set(server, (server.address() as { port: number }).port);
}

const raw = (server: unknown, request: string) => new Promise<string>((resolve, reject) => {
  const socket = connect(ports.get(server)!, "127.0.0.1", () => socket.write(request));
  let data = "";
  socket.on("data", (chunk) => { data += chunk; });
  socket.on("end", () => resolve(data));
  socket.on("error", reject);
});
const post = (host: string) => "POST /notes HTTP/1.1\r\nHost: " + host + "\r\nX-Request-ID: r1\r\nContent-Type: application/json\r\nContent-Length: 13\r\nConnection: close\r\n\r\n{\"text\":\"hi\"}";

for (const host of ["a:b@127.0.0.1", "a b"]) {
  const got = await raw(custom, post(host));
  if (!got.startsWith("HTTP/1.1 400") || !got.includes("{\"error\":{\"code\":\"bad_request\",\"message\":\"bad request\"}}")) throw new Error("Host " + host + " did not reach the error writer: " + got);
  const def = await raw(plain, post(host));
  if (!def.startsWith("HTTP/1.1 400") || !def.includes("{\"message\":\"bad request\"}")) throw new Error("Host " + host + " did not get the default error body: " + def);
}
if (JSON.stringify(seen) !== JSON.stringify(["bad_request POST r1", "bad_request POST r1"])) throw new Error("error writer saw " + JSON.stringify(seen));
for (const server of [custom, plain]) {
  const good = await raw(server, post("localhost"));
  if (!good.startsWith("HTTP/1.1 200")) throw new Error("server stopped serving after a bad request: " + good);
  server.close();
}
console.log("OK");
`)
}
