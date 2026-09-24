package gents

import "testing"

func TestTSNodeAdapterRejectsMalformedHost(t *testing.T) {
	runTSSchema(t, `
package app
message Note { text: string }
service Notes { create(Note) -> Note @post("/notes") }
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
