package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const clientBodySchema = `package app

message Ping { n: int32 }
message Pong { note: string }

service Echo {
  base_path: "/e"
  ping(Ping) -> Pong @post("/ping")
  watch(Ping) -> Pong @get("/watch") @stream
}
`

func TestTSClientBodyLimitsAndSSEFraming(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onklang.Parse(clientBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), strings.ReplaceAll(string(GenerateClient(file)), `from "./types.js"`, `from "./types.ts"`))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { EchoClient } from "./client.ts";

const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };
const enc = new TextEncoder();

const clientFor = (make: () => Response, options: { maxResponseBodyBytes?: number; maxSSELineBytes?: number } = {}) =>
  new EchoClient("http://api", { fetch: (async () => make()) as typeof fetch, ...options });

const small = JSON.stringify({ note: "hello" });
check((await clientFor(() => new Response(small, { headers: { "content-length": String(small.length) } })).ping({ n: 1 })).note === "hello", "a body with a content length");
check((await clientFor(() => new Response(new ReadableStream({ start(c) { c.enqueue(enc.encode(small)); c.close(); } }))).ping({ n: 1 })).note === "hello", "a streamed body without a content length");

const big = JSON.stringify({ note: "x".repeat(2000) });
const rejects = async (make: () => Response, limit: number, what: string) => {
  try { await clientFor(make, { maxResponseBodyBytes: limit }).ping({ n: 1 }); } catch (e) { check(String(e).includes("exceeds"), what + ": " + e); return; }
  throw new Error(what + ": a body over the limit was accepted");
};
await rejects(() => new Response(big, { headers: { "content-length": String(big.length) } }), 100, "declared length over the limit");
await rejects(() => new Response(new ReadableStream({ start(c) { c.enqueue(enc.encode(big)); c.close(); } })), 100, "streamed body over the limit");
await rejects(() => new Response(big, { headers: { "content-length": "20", "content-encoding": "gzip" } }), 100, "a small declared length must not hide a large decoded body");
await rejects(() => new Response(new ReadableStream({ start(c) { c.enqueue(enc.encode(big)); c.close(); } }), { headers: { "content-length": "20" } }), 100, "a small declared length must not hide a larger body");

const sse = (text: string, chunk: number) => () => new Response(new ReadableStream({
  start(c) { const bytes = enc.encode(text); for (let i = 0; i < bytes.length; i += chunk) c.enqueue(bytes.slice(i, i + chunk)); c.close(); },
}), { headers: { "content-type": "text/event-stream" } });
const events = Array.from({ length: 200 }, (_, i) => "data: " + JSON.stringify({ note: "é" + i }) + "\n\n").join("");
for (const chunk of [1, 7, 64, 100000]) {
  const seen: string[] = [];
  for await (const e of clientFor(sse(events, chunk)).watch({ n: 1 })) seen.push(e.note ?? "");
  check(seen.length === 200 && seen[0] === "é0" && seen[199] === "é199", "sse with " + chunk + "-byte chunks: " + seen.length);
}
const crlf = "event: tick\r\ndata: " + JSON.stringify({ note: "a" }) + "\r\n\r\n: comment\n\ndata: {\"note\":\r\ndata: \"b\"}\n\n";
const got: string[] = [];
for await (const e of clientFor(sse(crlf, 5)).watch({ n: 1 })) got.push(e.note ?? "");
check(got.join(",") === "a,b", "crlf, comments and multi-line data: " + got);

let limited = false;
try { for await (const _ of clientFor(sse("data: " + "é".repeat(100) + "\n\n", 16), { maxSSELineBytes: 100 }).watch({ n: 1 })) void _; } catch (e) { limited = String(e).includes("SSE line exceeds"); }
check(limited, "multibyte lines are measured in bytes");
let fine = true;
try { for await (const _ of clientFor(sse("data: " + JSON.stringify({ note: "é".repeat(40) }) + "\n\n", 16), { maxSSELineBytes: 120 }).watch({ n: 1 })) void _; } catch { fine = false; }
check(fine, "a line within the byte limit must pass");
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
}
