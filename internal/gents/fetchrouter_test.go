package gents

import "testing"

func TestTSFetchHandlerRoutesAndRejects(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  id: string;
  text: string;
}

interface Notes {
  @get
  @route("/notes/{id}")
  get(...Note): Note;

  @put
  @route("/notes/{id}")
  update(...Note): Note;
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
const malformed = await handle(new Request("http://x/notes/%E0%A4%A"));
if (malformed.status !== 404) throw new Error("a malformed percent-escape must not throw: " + malformed.status);
console.log("OK");
`)
}

func TestTSNodeHandlerSurvivesMalformedPercentEscapes(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  id: string;
  text: string;
}

interface Notes {
  @get
  @route("/notes/{id}")
  get(...Note): Note;
}
`, `
import http from "node:http";
import { attachNotesNodeHandlers } from "./server.ts";

const server = http.createServer();
attachNotesNodeHandlers(server, { get: async (n: any) => ({ ...n, text: "got" }) } as any);
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const { port } = server.address() as { port: number };
const bad = await fetch("http://127.0.0.1:" + port + "/notes/%E0%A4%A");
if (bad.status !== 404) throw new Error("malformed escape: " + bad.status);
const good = await fetch("http://127.0.0.1:" + port + "/notes/1");
if (good.status !== 200 || (await good.json()).text !== "got") throw new Error("server must keep working: " + good.status);
server.close();
console.log("OK");
`)
}
