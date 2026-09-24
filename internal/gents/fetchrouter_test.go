package gents

import "testing"

func TestTSFetchHandlerRoutesAndRejects(t *testing.T) {
	runTSSchema(t, `
package app
message Note { id: string  text: string }
service Notes {
  get(Note) -> Note @get("/notes/{id}")
  update(Note) -> Note @put("/notes/{id}")
}
`, `
import { createNotesFetchHandler } from "./server.ts";

const handle = createNotesFetchHandler({ get: async (n) => ({ ...n, text: "got" }), update: async (n) => n } as any);
const ok = await handle(new Request("http://x/notes/1"));
if (ok.status !== 200 || (await ok.json()).text !== "got") throw new Error("GET not routed");
const missing = await handle(new Request("http://x/other"));
if (missing.status !== 404) throw new Error("unknown path: " + missing.status);
const wrong = await handle(new Request("http://x/notes/1", { method: "DELETE" }));
if (wrong.status !== 405 || wrong.headers.get("Allow") !== "GET, PUT") throw new Error("wrong method: " + wrong.status + " " + wrong.headers.get("Allow"));
const options = await handle(new Request("http://x/notes/1", { method: "OPTIONS" }));
if (options.status !== 204 || options.headers.get("Allow") !== "GET, PUT") throw new Error("OPTIONS: " + options.status);
console.log("OK");
`)
}
