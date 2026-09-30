package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const scopesSchema = `
package app

message Item { id: string }
message GetItem { id: string }

service Items {
  get(GetItem) -> Item @get("/items/{id}") @requires("items:read")
  remove(GetItem) -> Item @delete("/items/{id}") @requires("items:read", "items:write")
  ping(GetItem) -> Item @get("/ping/{id}")
}
`

func TestTSRouteDescriptorsCarryScopes(t *testing.T) {
	out := string(GenerateServer(compileTSSchema(t, scopesSchema)))
	for _, want := range []string{
		`scopes?: readonly string[];`,
		`scopes: ["items:read"],`,
		`scopes: ["items:read", "items:write"],`,
		`export function requireScopes(`,
		`export function createItemsFetchHandler(handler: ItemsHandler, options?: ServerOptions)`,
		`export function attachItemsNodeHandlers(httpServer: NodeServerLike, handler: ItemsHandler, options?: ServerOptions): void {`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "scopes: [") != 2 {
		t.Fatalf("only routes that declare @requires carry scopes:\n%s", out)
	}
}

func TestTSServerEnforcesScopesThroughTheAuthorizeHook(t *testing.T) {
	runTSSchema(t, scopesSchema, `import { createItemsFetchHandler, requireScopes } from "./server.ts";

const handler = {
  async get(req: { id?: string }) { return { id: req.id }; },
  async remove(req: { id?: string }) { return { id: req.id }; },
  async ping(req: { id?: string }) { return { id: req.id }; },
};

function check(ok: boolean, what: string): void {
  if (!ok) { console.error(what); process.exit(1); }
}

const held = (req: Request) => (req.headers.get("x-scopes") ?? "").split(",").filter(Boolean);
const guarded = createItemsFetchHandler(handler, { authorize: requireScopes(held) });
const open = createItemsFetchHandler(handler);

async function call(fetcher: (req: Request) => Promise<Response>, method: string, path: string, scopes?: string) {
  const res = await fetcher(new Request("http://x" + path, { method, headers: scopes ? { "x-scopes": scopes } : {} }));
  return { status: res.status, body: await res.text() };
}

const ok = await call(guarded, "GET", "/items/7", "items:read");
check(ok.status === 200, "holding the scope must pass: " + ok.status);

const forbidden = await call(guarded, "DELETE", "/items/7", "items:read");
check(forbidden.status === 403 && forbidden.body.includes("missing required scope: items:write"), "missing scope: " + forbidden.status + forbidden.body);

const none = await call(guarded, "DELETE", "/items/7");
check(none.status === 403 && none.body.includes("items:read, items:write"), "every missing scope is listed: " + none.body);

const pingOpen = await call(guarded, "GET", "/ping/1");
check(pingOpen.status === 200, "routes without @requires stay open: " + pingOpen.status);

const unguarded = await call(open, "DELETE", "/items/7");
check(unguarded.status === 200, "without an authorize hook nothing is enforced: " + unguarded.status);

const custom = createItemsFetchHandler(handler, {
  authorize: (req, route) => {
    if (route.scopes && !req.headers.get("authorization")) throw new Error("not an HttpError");
  },
});
const crashed = await call(custom, "GET", "/items/7");
check(crashed.status === 500, "a thrown non-HttpError is a 500: " + crashed.status);
console.log("OK");
`)
}

func TestTSServerWithScopesTypeChecksStrict(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileTSSchema(t, scopesSchema)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc type check failed: %v\n%s", err, out)
	}
}
