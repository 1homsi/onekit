package gents

import "testing"

const tsWildcardSchema = `package app

message FileRef { path: string }
message Content { path: string }
message Empty {}

service Files {
  base_path: "/files"
  read(FileRef) -> Content @get("/{path...}")
  root(Empty) -> Content @get("")
}
`

func TestTSWildcardPathsAndEmptyRoutes(t *testing.T) {
	runTSSchema(t, tsWildcardSchema, `
import { createFilesFetchHandler } from "./server.ts";
import { FilesClient } from "./client.ts";

const handler = createFilesFetchHandler({
  async read(req) { return { path: req.path }; },
  async root() { return { path: "ROOT" }; },
});

const raw = async (path: string) => {
  const res = await handler(new Request("http://x" + path));
  return res.status + " " + (await res.text());
};
const expectations: Record<string, string> = {
  "/files/a.txt": '200 {"path":"a.txt"}',
  "/files/dir/sub/b.txt": '200 {"path":"dir/sub/b.txt"}',
  "/files/sp%20ace/x%2By": '200 {"path":"sp ace/x+y"}',
  "/files": '200 {"path":"ROOT"}',
};
for (const [path, want] of Object.entries(expectations)) {
  const got = await raw(path);
  if (got !== want) throw new Error(path + ": " + got + " want " + want);
}

const client = new FilesClient("http://x", { fetch: (input, init) => handler(new Request(input, init)) });
for (const path of ["a.txt", "dir/sub/b.txt", "sp ace/x+y", "100%/ü.txt"]) {
  const got = await client.read({ path });
  if (got.path !== path) throw new Error("client " + path + " -> " + got.path);
}
let rejected = false;
try { await client.read({ path: "a/../../admin" }); } catch { rejected = true; }
if (!rejected) throw new Error("dot segments must be rejected");
if ((await client.root({})).path !== "ROOT") throw new Error("root");
console.log("OK");
`)
}
