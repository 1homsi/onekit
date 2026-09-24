package gents

import "testing"

func TestTSClientThrowsRequestValidationError(t *testing.T) {
	runTSSchema(t, `
package app
message Note { text: string @len(1, 5)  email: string @email }
service Notes { create(Note) -> Note @post("/notes") }
`, `
import { NotesClient, RequestValidationError } from "./client.ts";

const client = new NotesClient("http://127.0.0.1:1");
try {
  await client.create({ text: "far too long", email: "nope" });
  throw new Error("invalid request was sent");
} catch (err) {
  if (!(err instanceof RequestValidationError) || !(err instanceof TypeError)) throw err;
  if (err.violations.length !== 2) throw new Error("violations: " + err.violations.join(", "));
}
console.log("OK");
`)
}
