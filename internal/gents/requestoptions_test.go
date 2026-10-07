package gents

import "testing"

func TestTSClientPerCallOptions(t *testing.T) {
	runTSSchema(t, `
package app
message Note { id: string  text: string }
service Notes {
  get(Note) -> Note @get("/notes/{id}")
  create(Note) -> Note @post("/notes")
}
`, `
import { NotesClient } from "./client.ts";

const seen: RequestInit[] = [];
const client = new NotesClient("http://api", {
  defaultHeaders: { "X-Base": "1" },
  fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
    seen.push(init ?? {});
    return new Response(JSON.stringify({ id: "1", text: "t" }), { status: 200 });
  }) as typeof fetch,
});
const controller = new AbortController();
await client.get({ id: "1", text: "" }, { signal: controller.signal, headers: { "Idempotency-Key": "k1" } });
await client.create({ id: "1", text: "t" }, { headers: { "Idempotency-Key": "k2" } });
const [get, create] = seen.map((init) => ({ headers: init.headers as Record<string, string>, signal: init.signal }));
controller.abort();
if (!get.signal?.aborted || get.headers["Idempotency-Key"] !== "k1" || get.headers["X-Base"] !== "1") throw new Error("get options lost");
if (create.headers["Idempotency-Key"] !== "k2" || create.headers["Content-Type"] !== "application/json") throw new Error("create options lost");
console.log("OK");
`)
}

func TestTSClientDynamicHeadersAndResponseHooks(t *testing.T) {
	runTSSchema(t, `
package app
message Note { id: string  text: string }
service Notes {
  get(Note) -> Note @get("/notes/{id}")
  create(Note) -> Note @post("/notes")
}
`, `
import { NotesClient } from "./client.ts";
import { ApiError } from "./client.ts";

let token = "t1";
const seen: Record<string, string>[] = [];
const responses: string[] = [];
const unauthorized: string[] = [];
let status = 200;
const client = new NotesClient("http://api", {
  defaultHeaders: async () => ({ Authorization: "Bearer " + token, "X-Override": "default" }),
  onResponse: (res, request) => { responses.push(request.method + " " + res.status); },
  onUnauthorized: (_res, request) => { unauthorized.push(request.url); },
  fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
    seen.push(init?.headers as Record<string, string>);
    return new Response(JSON.stringify({ id: "1", text: "t" }), { status });
  }) as typeof fetch,
});
await client.get({ id: "1", text: "" });
token = "t2";
await client.create({ id: "1", text: "t" }, { headers: { "X-Override": "call" } });
if (seen[0].Authorization !== "Bearer t1" || seen[1].Authorization !== "Bearer t2") throw new Error("the header function must run before every request");
if (seen[1]["X-Override"] !== "call" || seen[1]["Content-Type"] !== "application/json") throw new Error("per-call headers must win and Content-Type must survive");
if (responses.join() !== "GET 200,POST 200") throw new Error("onResponse: " + responses.join());
status = 401;
try {
  await client.get({ id: "2", text: "" });
  throw new Error("expected an ApiError");
} catch (err) {
  if (!(err instanceof ApiError) || err.statusCode !== 401) throw err;
}
if (unauthorized.join() !== "http://api/notes/2") throw new Error("onUnauthorized: " + unauthorized.join());
console.log("OK");
`)
}

func TestTSJSONObjectIsARecordAndValidated(t *testing.T) {
	runTSSchema(t, `
package app
message Doc { id: string  settings: json @object  any: json }
service Docs { put(Doc) -> Doc @put("/docs") }
`, `
import type { Doc } from "./types.ts";
import { validateDoc } from "./types.ts";

const ok: Doc = { id: "1", settings: { a: 1, nested: { b: [1] } }, any: [1] };
const settings: Record<string, unknown> | undefined = ok.settings;
if (!settings || validateDoc(ok).length !== 0) throw new Error("an object must validate");
for (const bad of [[1], "x", 5]) {
  if (validateDoc({ id: "1", settings: bad as any }).length === 0) throw new Error("a non-object must not validate: " + JSON.stringify(bad));
}
console.log("OK");
`)
}
