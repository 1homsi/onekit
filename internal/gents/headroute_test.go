package gents

import "testing"

func TestTSServerAnswersHeadOnGetRoutes(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  id: string;
  text: string;
}

interface Notes {
  @get
  @route("/notes/{id}")
  get(...Note): Note;

  @post
  @route("/notes")
  create(...Note): Note;
}
`, `
import http from "node:http";
import { createNotesFetchHandler, attachNotesNodeHandlers } from "./server.ts";

const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };
const impl = { get: async (n: any) => ({ ...n, text: "got" }), create: async (n: any) => n } as any;

const handle = createNotesFetchHandler(impl);
const head = await handle(new Request("http://x/notes/1", { method: "HEAD" }));
check(head.status === 200, "HEAD on a GET route: " + head.status);
check((await head.text()) === "", "a HEAD response has no body");
check(head.headers.get("Content-Type") === "application/json", "a HEAD response keeps the GET headers");
const headPost = await handle(new Request("http://x/notes", { method: "HEAD" }));
check(headPost.status === 405, "HEAD on a POST-only route stays 405: " + headPost.status);

const server = http.createServer();
attachNotesNodeHandlers(server, impl);
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const { port } = server.address() as { port: number };
const viaNode = await fetch("http://127.0.0.1:" + port + "/notes/1", { method: "HEAD" });
check(viaNode.status === 200 && (await viaNode.text()) === "", "HEAD through the Node adapter: " + viaNode.status);
const viaNodeMissing = await fetch("http://127.0.0.1:" + port + "/other", { method: "HEAD" });
check(viaNodeMissing.status === 404, "HEAD on an unknown path: " + viaNodeMissing.status);
server.close();
console.log("OK");
`)
}
