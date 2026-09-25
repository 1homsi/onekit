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
