package gents

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const tsErrorSchema = `package app

message Thing {
  id: int64
  page: int32 @query
}
message Draft {
  id: int64
  name: string @len(2, 10)
}
message Done { ok: bool }

service Things {
  base_path: "/v1"
  headers: { "X-Tenant": string @required }
  get(Thing) -> Done @get("/things/{id}")
  save(Draft) -> Done @post("/things/save")
}
`

func TestTSServerOnErrorAndClientParsingCoverEveryErrorPath(t *testing.T) {
	runTSSchema(t, tsErrorSchema, `
import { createThingsFetchHandler, HttpError } from "./server.ts";
import type { ThingsHandler, ServerErrorInfo } from "./server.ts";
import { ThingsClient, ApiError } from "./client.ts";

const handler: ThingsHandler = {
  async get(req) {
    if (req.id === "1") return { ok: true };
    throw new Error("database exploded: secret detail");
  },
  async save() { return { ok: true }; },
};

let seq = 0;
const envelope = (info: ServerErrorInfo, req: Request) =>
  new Response(JSON.stringify({ error: { code: info.code, message: info.message, request_id: "req-" + ++seq, field: info.field, violations: info.violations } }), {
    status: info.status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req-" + seq },
  });

const custom = createThingsFetchHandler(handler, { onError: envelope });
const plain = createThingsFetchHandler(handler);

async function call(h: (req: Request) => Promise<Response>, method: string, path: string, body?: string, headers: Record<string, string> = { "x-tenant": "t1" }) {
  const res = await h(new Request("http://x" + path, { method, body, headers: { "content-type": "application/json", ...headers } }));
  const text = await res.text();
  let json: any; try { json = JSON.parse(text); } catch {}
  return { status: res.status, json, allow: res.headers.get("allow") };
}

function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(name + ": got " + JSON.stringify(got) + ", want " + JSON.stringify(want));
}

let r = await call(custom, "POST", "/v1/things/save", "{not json");
expect("bad body", [r.status, r.json.error.code], [400, "invalid_request_body"]);
r = await call(custom, "GET", "/v1/things/abc");
expect("bad path", [r.status, r.json.error.code, r.json.error.field], [400, "invalid_path_parameter", "id"]);
r = await call(custom, "GET", "/v1/things/1?page=x");
expect("bad query", [r.status, r.json.error.code, r.json.error.field], [400, "invalid_query_parameter", "page"]);
r = await call(custom, "GET", "/v1/things/1", undefined, {});
expect("missing header", [r.status, r.json.error.code, r.json.error.field], [400, "missing_header", "X-Tenant"]);
r = await call(custom, "POST", "/v1/things/save", JSON.stringify({ id: "1", name: "x" }));
expect("validation", [r.status, r.json.error.code, r.json.error.violations.length > 0], [400, "validation_failed", true]);
r = await call(custom, "GET", "/v1/things/3");
expect("unknown error", [r.status, r.json.error.code, r.json.error.message], [500, "internal", "internal server error"]);
if (JSON.stringify(r.json).includes("secret")) throw new Error("the cause must never be sent");
r = await call(custom, "GET", "/nope");
expect("unmatched", [r.status, r.json.error.code], [404, "not_found"]);
r = await call(custom, "DELETE", "/v1/things/save");
expect("wrong method", [r.status, r.json.error.code, r.allow], [405, "method_not_allowed", "GET, POST"]);

r = await call(plain, "POST", "/v1/things/save", "{not json");
expect("default body", r.json, { message: "invalid request body" });
r = await call(plain, "GET", "/nope");
expect("default 404", r.json, { message: "not found" });

const client = new ThingsClient("http://x", {
  fetch: ((url: string, init: RequestInit) => custom(new Request(url, init))) as typeof fetch,
  defaultHeaders: { "X-Tenant": "t1" },
});
try {
  await client.get({ id: "3" });
  throw new Error("expected a failure");
} catch (err) {
  if (!(err instanceof ApiError)) throw err;
  expect("client status", err.statusCode, 500);
  expect("client code", err.code, "internal");
  expect("client message", err.message, "internal server error");
  if (!err.requestId || !err.requestId.startsWith("req-")) throw new Error("request id missing: " + err.requestId);
  expect("client json kept", (err.json as any).error.code, "internal");
}

const flat = new ThingsClient("http://x", {
  fetch: (async () => new Response(JSON.stringify({ errno: 7, text: "custom" }), { status: 418 })) as unknown as typeof fetch,
  defaultHeaders: { "X-Tenant": "t1" },
  errorParser: ({ json }) => ({ code: String((json as any).errno), message: (json as any).text }),
});
try { await flat.get({ id: "1" }); throw new Error("expected a failure"); } catch (err) {
  if (!(err instanceof ApiError)) throw err;
  expect("custom parser", [err.code, err.message], ["7", "custom"]);
}
const text = new ThingsClient("http://x", {
  fetch: (async () => new Response("upstream down", { status: 502 })) as unknown as typeof fetch,
  defaultHeaders: { "X-Tenant": "t1" },
});
try { await text.get({ id: "1" }); throw new Error("expected a failure"); } catch (err) {
  if (!(err instanceof ApiError)) throw err;
  expect("non-json body", [err.message, err.json, err.code], ["unexpected status 502: upstream down", undefined, undefined]);
}
console.log("OK");
`)
}

func TestGeneratedTSErrorHooksTypeCheckStrictly(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileTSSchema(t, tsErrorSchema)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "exactOptionalPropertyTypes": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"],
    "types": []
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated error hooks do not type-check: %v\n%s", err, out)
	}
}
