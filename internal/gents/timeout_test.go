package gents

import "testing"

func TestTSClientTimesOutSlowRequests(t *testing.T) {
	runTSSchema(t, `
package app
message Note { id: string }
service Notes { get(Note) -> Note @get("/notes/{id}") }
`, `
import { NotesClient } from "./client.ts";

const hang = (async (_input: RequestInfo | URL, init?: RequestInit) =>
  new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener("abort", () => reject(init.signal?.reason));
  })) as typeof fetch;
const client = new NotesClient("http://api", { fetch: hang, timeoutMs: 50 });
const keepAlive = setInterval(() => {}, 1000);
const started = Date.now();
try {
  await client.get({ id: "1" });
  throw new Error("request never timed out");
} catch (error) {
  if ((error as Error).name !== "TimeoutError") throw error;
}
try {
  await client.get({ id: "1" }, { timeoutMs: 20 });
} catch (error) {
  if ((error as Error).name !== "TimeoutError") throw error;
}
if (Date.now() - started > 2000) throw new Error("timeouts took too long");
clearInterval(keepAlive);
console.log("OK");
`)
}
