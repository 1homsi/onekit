package gents

import "testing"

const tsMetaSchema = `package app

message Doc { slug: string }
message Done { ok: bool }

service Docs {
  base_path: "/v1"
  edit(Doc) -> Done @post("/apps/{slug}/edit") @meta("guard", "app/edit/:slug") @meta("audit.event", "app.update")
  watch(Doc) -> Done @get("/apps/{slug}/watch") @stream @meta("guard", "app/use/:slug")
  health(Doc) -> Done @get("/health/{slug}")
}
`

func TestTSRouteMetadataReachesAuthorizeAndHandlers(t *testing.T) {
	runTSSchema(t, tsMetaSchema, `
import { createDocsFetchHandler, createDocsRoutes } from "./server.ts";
import type { DocsHandler, RouteDescriptor } from "./server.ts";

const seen: string[] = [];
const handler: DocsHandler = {
  async edit(_req, context) {
    seen.push("handler:" + context.meta?.["audit.event"]);
    return { ok: true };
  },
  watch(_req, context) {
    seen.push("stream:" + context.meta?.["guard"]);
    return new ReadableStream({ start(c) { c.enqueue({ ok: true }); c.close(); } });
  },
  async health(_req, context) {
    seen.push("health:" + String(context.meta));
    return { ok: true };
  },
};

const routes: RouteDescriptor[] = createDocsRoutes(handler);
const byPath = Object.fromEntries(routes.map((r) => [r.method + " " + r.path, r]));
if (JSON.stringify(byPath["POST /v1/apps/{slug}/edit"]?.meta) !== JSON.stringify({ guard: "app/edit/:slug", "audit.event": "app.update" })) throw new Error("edit meta: " + JSON.stringify(byPath));
if (byPath["GET /v1/health/{slug}"]?.meta !== undefined) throw new Error("a route without @meta must have no meta");

const guarded: string[] = [];
const fetchHandler = createDocsFetchHandler(handler, {
  authorize: (req, route) => {
    const rule = route.meta?.["guard"];
    if (rule) guarded.push(rule + " " + new URL(req.url).pathname);
  },
});
const post = await fetchHandler(new Request("http://x/v1/apps/mine/edit", { method: "POST", body: JSON.stringify({}), headers: { "content-type": "application/json" } }));
if (post.status !== 200) throw new Error("edit status " + post.status);
const stream = await fetchHandler(new Request("http://x/v1/apps/mine/watch"));
await stream.text();
await fetchHandler(new Request("http://x/v1/health/x"));
if (JSON.stringify(guarded) !== JSON.stringify(["app/edit/:slug /v1/apps/mine/edit", "app/use/:slug /v1/apps/mine/watch"])) throw new Error("guards: " + JSON.stringify(guarded));
if (JSON.stringify(seen) !== JSON.stringify(["handler:app.update", "stream:app/use/:slug", "health:undefined"])) throw new Error("handlers: " + JSON.stringify(seen));
console.log("OK");
`)
}
