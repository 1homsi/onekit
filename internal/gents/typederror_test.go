package gents

import "testing"

func TestTSClientThrowsTypedApiErrors(t *testing.T) {
	runTSSchema(t, `
package app
message R { id: string }
message NotFoundError @status(404) { resource_id: string  message: string }
service S { get(R) -> R | NotFoundError @get("/r/{id}") }
`, `
import { ApiError, SClient, TypedApiError } from "./client.ts";

const client = new SClient("http://api", {
  fetch: (async () => new Response(JSON.stringify({ resource_id: "7", message: "no such thing" }), { status: 404 })) as typeof fetch,
});
try {
  await client.get({ id: "7" });
  throw new Error("expected an error");
} catch (err) {
  if (!(err instanceof TypedApiError) || !(err instanceof ApiError) || !(err instanceof Error)) throw err;
  const typed = err as TypedApiError<{ resourceId: string }> & { resourceId: string };
  if (typed.errorType !== "NotFoundError" || typed.statusCode !== 404 || typed.data.resourceId !== "7" || typed.resourceId !== "7" || typed.message !== "no such thing" || !typed.stack) {
    throw new Error("typed error lost data: " + JSON.stringify({ ...typed, errorType: typed.errorType }));
  }
}
console.log("OK");
`)
}
