package gents

import "testing"

func TestTSNodeAdapterAbortsRequestWhenClientLeaves(t *testing.T) {
	runTSSchema(t, `
package app
message Job { id: string }
service Jobs { wait(Job) -> Job @get("/jobs/{id}") }
`, `
import { createServer, request } from "node:http";
import { attachJobsNodeHandlers } from "./server.ts";

let aborted!: () => void;
const sawAbort = new Promise<void>((resolve) => { aborted = resolve; });
const server = createServer();
attachJobsNodeHandlers(server, {
  wait: (job: { id: string }, ctx: { request: Request }) => new Promise((resolve) => {
    ctx.request.signal.addEventListener("abort", () => { aborted(); resolve(job); });
  }),
} as any);
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = (server.address() as { port: number }).port;
const req = request({ host: "127.0.0.1", port, path: "/jobs/1" });
req.on("error", () => {});
req.end();
setTimeout(() => req.destroy(), 100);
const timer = setTimeout(() => { console.error("handler never saw the abort"); process.exit(1); }, 3000);
await sawAbort;
clearTimeout(timer);
server.close();
console.log("OK");
`)
}
