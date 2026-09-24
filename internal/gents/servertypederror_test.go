package gents

import "testing"

func TestTSServerTypedErrorHelpers(t *testing.T) {
	runTSSchema(t, `
package app
message R { id: string }
message NotFoundError @status(404) { resource_id: string }
service S { get(R) -> R | NotFoundError @get("/r/{id}") }
`, `
import { createSFetchHandler, httpErrorFromNotFoundError } from "./server.ts";

const handle = createSFetchHandler({
  get: async (req) => { throw httpErrorFromNotFoundError({ resourceId: req.id }); },
} as any);
const res = await handle(new Request("http://x/r/7"));
const body = await res.json();
if (res.status !== 404 || body.resource_id !== "7") throw new Error("typed error response: " + res.status + " " + JSON.stringify(body));
console.log("OK");
`)
}
