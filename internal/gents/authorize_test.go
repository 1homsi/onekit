package gents

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const authorizeTSSchema = `namespace app;

@Onkt.principal
model Principal {
  user_id: string;
  roles: string[];
  org: string;
}

model DeleteDoc {
  id: string;
  owner_org: string;
}

model WatchDocs {
  org: string;
}

model Doc {
  id: string;
}

model Ack {
  ok: boolean;
}

@route("/v1")
interface Docs {
  @post
  @route("/docs/delete")
  @Onkt.authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
  @Onkt.authorize("size(auth.user_id) > 0", "sign in first")
  delete(...DeleteDoc): Ack;

  @post
  @route("/docs/open")
  open(...DeleteDoc): Ack;

  @get
  @route("/docs/watch")
  @Onkt.stream
  @Onkt.authorize("auth.org == req.org", "not your organization")
  watch(...WatchDocs): Doc;
}
`

func TestTSServerEnforcesAuthorize(t *testing.T) {
	runTSSchema(t, authorizeTSSchema, `
import { createDocsFetchHandler, HttpError } from "./server.ts";
import type { DocsHandler } from "./server.ts";

const handler: DocsHandler = {
  async delete(_req, context) {
    if (!context.principal || context.principal.userId === "") throw new Error("handler should see the principal");
    return { ok: true };
  },
  async open() {
    return { ok: true };
  },
  watch(_req, _context) {
    return new ReadableStream({ start(controller) { controller.enqueue({ id: "d1" }); controller.close(); } });
  },
};

const people: Record<string, any> = {
  admin: { userId: "u1", roles: ["admin"], org: "acme" },
  member: { userId: "u2", roles: [], org: "acme" },
  anonymous: { userId: "", roles: [], org: "" },
};

const configured = createDocsFetchHandler(handler, {
  principal: (req) => {
    const who = req.headers.get("x-user") ?? "";
    if (who === "teapot") throw new HttpError(418, { message: "short and stout" });
    return people[who];
  },
});
// The session store is unreachable: the lookup fails for a reason that says
// nothing about the caller's credentials.
const causes: unknown[] = [];
const outageHandler = createDocsFetchHandler(handler, {
  principal: async () => {
    throw new Error("connect ECONNREFUSED 127.0.0.1:5432");
  },
  onError: (info) => {
    causes.push(info.cause);
    return Response.json({ message: info.message }, { status: info.status });
  },
});
const bare = createDocsFetchHandler(handler);

async function call(h: (req: Request) => Promise<Response>, path: string, who: string, body?: unknown) {
  const init: RequestInit = { method: body === undefined ? "GET" : "POST", headers: { "x-user": who, "content-type": "application/json" } };
  if (body !== undefined) init.body = JSON.stringify(body);
  const res = await h(new Request("http://x" + path, init));
  let json: any = undefined;
  try { json = await res.clone().json(); } catch {}
  return { status: res.status, json };
}

function expect(name: string, got: number, want: number) {
  if (got !== want) throw new Error(name + ": status " + got + ", want " + want);
}

expect("admin may delete anything", (await call(configured, "/v1/docs/delete", "admin", { id: "1", owner_org: "other" })).status, 200);
expect("member may delete in their org", (await call(configured, "/v1/docs/delete", "member", { id: "1", owner_org: "acme" })).status, 200);

const forbidden = await call(configured, "/v1/docs/delete", "member", { id: "1", owner_org: "other" });
expect("member may not delete elsewhere", forbidden.status, 403);
if (forbidden.json.message !== "not allowed to delete this document") throw new Error("message: " + JSON.stringify(forbidden.json));

const both = await call(configured, "/v1/docs/delete", "anonymous", { id: "1", owner_org: "acme" });
expect("every failed rule is reported", both.status, 403);
if (both.json.violations.length !== 2) throw new Error("violations: " + JSON.stringify(both.json));

expect("unknown caller is unauthorized", (await call(configured, "/v1/docs/delete", "stranger", { id: "1" })).status, 401);
expect("the resolver controls the status", (await call(configured, "/v1/docs/delete", "teapot", { id: "1" })).status, 418);
const outage = await call(outageHandler, "/v1/docs/delete", "admin", { id: "1" });
expect("a failing resolver is a server fault, not a signed-out caller", outage.status, 500);
if (outage.json.message !== "internal server error") throw new Error("message: " + JSON.stringify(outage.json));
if (!(causes[0] instanceof Error) || !causes[0].message.includes("ECONNREFUSED")) throw new Error("onError should see the resolver's error, got " + String(causes[0]));
expect("methods without @authorize never ask who the caller is", (await call(configured, "/v1/docs/open", "stranger", { id: "1" })).status, 200);
expect("invalid requests are still rejected first", (await call(configured, "/v1/docs/delete", "admin", { id: 5 })).status, 400);
expect("stream without a caller is unauthorized", (await call(configured, "/v1/docs/watch?org=acme", "stranger")).status, 401);
expect("stream for the wrong organization is forbidden", (await call(configured, "/v1/docs/watch?org=other", "member")).status, 403);
expect("a missing principal resolver fails closed", (await call(bare, "/v1/docs/delete", "admin", { id: "1" })).status, 500);
expect("public methods work without a resolver", (await call(bare, "/v1/docs/open", "admin", { id: "1" })).status, 200);
console.log("OK");
`)
}

func TestGeneratedTSAuthorizeTypeChecksStrictly(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileTSSchema(t, authorizeTSSchema)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noUncheckedIndexedAccess": true,
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
		t.Fatalf("generated authorize code does not type-check: %v\n%s", err, out)
	}
}
