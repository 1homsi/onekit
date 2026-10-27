package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const clientBodySchema = `namespace app;

model Ping {
  n: int32;
}

model Pong {
  note: string;
}

@route("/e")
interface Echo {
  @post
  @route("/ping")
  ping(...Ping): Pong;

  @get
  @route("/watch")
  @Onkt.stream
  watch(...Ping): Pong;
}
`

func TestTSClientBodyLimitsAndSSEFraming(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onktest.Parse(clientBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
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

func TestTSClientPerCallHeaderOverridesTheDefaultWhateverTheCase(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onktest.Parse(clientBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), strings.ReplaceAll(string(GenerateClient(file)), `from "./types.js"`, `from "./types.ts"`))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { EchoClient } from "./client.ts";

let seen: Record<string, string> = {};
const client = new EchoClient("http://api", {
  defaultHeaders: { "X-API-Key": "default", "Accept-Language": "en" },
  fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
    seen = init?.headers as Record<string, string>;
    return new Response("{}");
  }) as typeof fetch,
});
await client.ping({ n: 1 }, { headers: { "x-api-key": "per-call" } });
const keys = Object.keys(seen).filter((k) => k.toLowerCase() === "x-api-key");
if (keys.length !== 1 || seen[keys[0]] !== "per-call") throw new Error("per-call header must replace the default: " + JSON.stringify(seen));
if (seen["Accept-Language"] !== "en" || seen["Content-Type"] !== "application/json") throw new Error("other headers must survive: " + JSON.stringify(seen));
await client.ping({ n: 1 }, { headers: { "X-API-Key": "same-case" } });
if (seen["X-API-Key"] !== "same-case") throw new Error("same-case override: " + JSON.stringify(seen));
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
}

func TestTSClientRejectsDotSegmentPathParameters(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onktest.Parse(`namespace app;

model Item {
  id: string;
}

model Count {
  n: int32;
}

interface Items {
  @get
  @route("/items/{id}")
  get(...Item): Item;

  @get
  @route("/watch/{id}")
  @Onkt.stream
  watch(...Item): Item;

  @get
  @route("/total/{n}")
  total(...Count): Item;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), strings.ReplaceAll(string(GenerateClient(file)), `from "./types.js"`, `from "./types.ts"`))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { ItemsClient } from "./client.ts";

const urls: string[] = [];
const client = new ItemsClient("http://api", { fetch: (async (input: RequestInfo | URL) => { urls.push(String(input)); return new Response("{}"); }) as typeof fetch });
for (const id of [".", ".."]) {
  let threw = false;
  try { await client.get({ id }); } catch (e) { threw = String(e).includes("dot segments"); }
  if (!threw) throw new Error("get(" + id + ") must be rejected");
  threw = false;
  try { for await (const _ of client.watch({ id })) void _; } catch (e) { threw = String(e).includes("dot segments"); }
  if (!threw) throw new Error("watch(" + id + ") must be rejected");
}
if (urls.length !== 0) throw new Error("a rejected id must not be fetched: " + urls);
for (const id of ["a", "a.b", "...", "a/b", ".a"]) await client.get({ id });
await client.total({ n: 0 });
if (urls.join(" ") !== "http://api/items/a http://api/items/a.b http://api/items/... http://api/items/a%2Fb http://api/items/.a http://api/total/0") throw new Error(urls.join(" "));
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
}
