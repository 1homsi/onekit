package gents

import "testing"

// validateRequests: false leaves every check to the server, on typed and raw
// routes alike, and a raw JSON route still decodes its declared errors.
func TestTSClientCanLeaveValidationToTheServer(t *testing.T) {
	runTSSchema(t, `namespace app;

model Note {
  @minLength(3)
  text: string;
}

model Query {
  @minLength(3)
  sql: string;
}

model RunRequest {
  @body query: Query;
}

model RunResult {
  rows: int32;
}

@error
model RunFailed {
  @statusCode _: 422;
  error: string;
  logs: string[];
}

interface Notes {
  @post
  @route("/notes")
  create(...Note): Note;

  @post
  @route("/run")
  @Onkt.http(#{ request: "json", response: "json" })
  run(...RunRequest): RunResult | RunFailed;
}
`, `
import { NotesClient, RequestValidationError, TypedApiError } from "./client.ts";

const sent: string[] = [];
const fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  sent.push(String(input) + " " + init?.body);
  if (String(input).endsWith("/run")) {
    return new Response(JSON.stringify({ error: "syntax error", logs: ["line 1"], result: null }), { status: 422 });
  }
  return new Response(JSON.stringify({ text: "no" }), { status: 200 });
};

const strict = new NotesClient("http://api", { fetch });
for (const call of [() => strict.create({ text: "no" }), () => strict.run({ query: { sql: "x" } })]) {
  try {
    await call();
    throw new Error("an invalid request was sent");
  } catch (err) {
    if (!(err instanceof RequestValidationError)) throw err;
  }
}
if (sent.length !== 0) throw new Error("sent: " + sent.join(", "));

const loose = new NotesClient("http://api", { fetch, validateRequests: false });
await loose.create({ text: "no" });
try {
  await loose.run({ query: { sql: "x" } });
  throw new Error("a 422 was not thrown");
} catch (err) {
  if (!(err instanceof TypedApiError) || err.statusCode !== 422 || err.errorType !== "RunFailed") throw err;
  const data = err.data as { error: string; logs: string[] };
  if (data.error !== "syntax error" || data.logs[0] !== "line 1") throw new Error("decoded: " + JSON.stringify(err.data));
}
if (sent.length !== 2) throw new Error("sent: " + sent.join(", "));
console.log("OK");
`)
}
