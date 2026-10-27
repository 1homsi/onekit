package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const rawHTTPSchema = `namespace app;

model Empty {}

model Bundle {
  slug: string;
  @query
  v: int32;
}

model Upload {
  slug: string;
  key: string;
}

@route("/apps")
interface Apps {
  @get
  @route("/{slug}/bundle.js")
  @Onkt.http(#{ contentType: "application/javascript" })
  bundle(...Bundle): Empty;

  @put
  @route("/{slug}/objects/{+key}")
  @Onkt.http
  put_object(...Upload): Empty;
}
`

func TestTSRawHTTPClientReturnsTheResponse(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onktest.Parse(rawHTTPSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	forNode := func(src []byte) string {
		return strings.ReplaceAll(string(src), `from "./types.js"`, `from "./types.ts"`)
	}
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), forNode(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { AppsClient } from "./client.ts";

const seen: { url: string; init: RequestInit }[] = [];
const client = new AppsClient("http://api", {
  fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
    seen.push({ url: String(input), init: init ?? {} });
    return new Response("console.log(1)", { status: 200, headers: { ETag: '"abc"', "Content-Type": "application/javascript" } });
  }) as typeof fetch,
  defaultHeaders: () => ({ Authorization: "Bearer t" }),
});
const res: Response = await client.bundle({ slug: "blog", v: 3 });
if (res.headers.get("ETag") !== '"abc"' || (await res.text()) !== "console.log(1)") throw new Error("the raw Response must reach the caller");
if (seen[0].url !== "http://api/apps/blog/bundle.js?v=3") throw new Error("url: " + seen[0].url);
const headers = seen[0].init.headers as Record<string, string>;
if (headers.Authorization !== "Bearer t") throw new Error("default headers must apply to raw calls");

const bytes = new Uint8Array([1, 2, 3]);
await client.putObject({ slug: "blog", key: "a/b c.png" }, { body: bytes, contentType: "image/png" });
if (seen[1].url !== "http://api/apps/blog/objects/a/b%20c.png" || seen[1].init.method !== "PUT" || seen[1].init.body !== bytes) throw new Error("upload: " + JSON.stringify(seen[1]));
if ((seen[1].init.headers as Record<string, string>)["Content-Type"] !== "image/png") throw new Error("content type lost");
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("tsc"); err == nil {
		writeFile(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","strict":true,"noEmit":true,"allowImportingTsExtensions":true,"lib":["ES2022","DOM"]},"files":["types.ts","client.ts"]}`)
		tsc := exec.Command("tsc", "-p", "tsconfig.json")
		tsc.Dir = dir
		if out, err := tsc.CombinedOutput(); err != nil {
			t.Fatalf("the raw client must type check: %v\n%s", err, out)
		}
	}
}

const rawRoutesSchema = `namespace app;

model Empty {}

model Slug {
  slug: string;
}

model DraftBody {
  source: string;
  minify: boolean;
}

model DraftRequest {
  slug: string;
  @body
  draft: DraftBody;
}

model Policy {
  allowed: boolean;
  reason: string;
}

model UploadResult {
  name: string;
  size: int32;
}

model ProxyRequest {
  slug: string;
  resource: string;
  path: string;
  @query
  trace: string;
}

@route("/apps")
interface Apps {
  @get
  @route("/{slug}/bundle.js")
  @Onkt.http(#{ contentType: "application/javascript" })
  bundle(...Slug): Empty;

  @post
  @route("/{slug}/bundle-draft.js")
  @Onkt.http(#{ contentType: "application/javascript", request: "json" })
  draft(...DraftRequest): Empty;

  @get
  @route("/{slug}/local-execution")
  @Onkt.http(#{ response: "json" })
  policy(...Slug): Policy;

  @get
  @route("/{slug}/local-execution/run")
  @Onkt.http(#{ response: "websocket" })
  run(...Slug): Empty;

  @get
  @route("/{slug}/oauth")
  @Onkt.http(#{ response: "redirect" })
  oauth(...Slug): Empty;

  @post
  @route("/{slug}/uploads")
  @Onkt.http(#{ request: "multipart", response: "json" })
  upload(...Slug): UploadResult;

  @Onkt.method("ANY")
  @route("/{slug}/proxy/{resource}/{+path}")
  @Onkt.http
  proxy(...ProxyRequest): Empty;
}
`

func TestTSRawRoutesByKind(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onktest.Parse(rawRoutesSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.tsp", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	forNode := func(src []byte) string {
		return strings.ReplaceAll(string(src), `from "./types.js"`, `from "./types.ts"`)
	}
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), forNode(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { AppsClient, appsBundleUrl, appsProxyUrl } from "./client.ts";

const seen: { url: string; init: RequestInit }[] = [];
const respond = (body: BodyInit | null, init: ResponseInit = { status: 200 }) =>
  (async (input: RequestInfo | URL, request?: RequestInit) => {
    seen.push({ url: String(input), init: request ?? {} });
    return new Response(body, init);
  }) as typeof fetch;
const make = (body: BodyInit | null, init?: ResponseInit) => new AppsClient("http://api", { fetch: respond(body, init) });
const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };

check(appsBundleUrl("http://api", { slug: "blog" }) === "http://api/apps/blog/bundle.js", "the plain url builder");
check(make(null).bundleUrl({ slug: "a b" }) === "http://api/apps/a%20b/bundle.js", "the client url builder");
check(appsProxyUrl("http://api", { slug: "blog", resource: "pg", path: "a/b c", trace: "t 1" }) === "http://api/apps/blog/proxy/pg/a/b%20c?trace=t+1", "wildcard tail and query in the url");
let threw = false;
try { appsProxyUrl("http://api", { slug: "blog", resource: "pg", path: "a/../b", trace: "" }); } catch { threw = true; }
check(threw, "dot segments in a wildcard tail must be rejected");

const client = make('{"allowed":true,"reason":"ok"}');
const policy = await client.policy({ slug: "blog" });
check(policy.allowed === true && policy.reason === "ok", "a json response is decoded into the message");

const draftClient = make("js");
const draftRes: Response = await draftClient.draft({ slug: "blog", draft: { source: "let a", minify: true } });
check(await draftRes.text() === "js", "a stream response is the Response");
check(seen[1].init.body === '{"source":"let a","minify":true}', "a json request sends the body field: " + seen[1].init.body);
check((seen[1].init.headers as Record<string, string>)["Content-Type"] === "application/json", "json content type");

check(!("run" in client) && !("oauth" in client), "redirect and websocket routes get no fetch method");
check(typeof client.runUrl === "function" && client.oauthUrl({ slug: "x" }) === "http://api/apps/x/oauth", "redirect and websocket routes get a url builder");

const form = new FormData();
form.set("file", new Blob(["data"]), "a.txt");
const uploadClient = make('{"name":"a.txt","size":4}');
const uploaded = await uploadClient.upload({ slug: "blog" }, { body: form });
check(uploaded.name === "a.txt" && uploaded.size === 4, "multipart json response");
check(seen[2].init.body === form && !(seen[2].init.headers as Record<string, string>)["Content-Type"], "multipart must leave the content type to fetch");

const proxy = make("x");
await proxy.proxy({ slug: "blog", resource: "pg", path: "a/b", trace: "" });
await proxy.proxy({ slug: "blog", resource: "pg", path: "a/b", trace: "" }, { method: "PATCH", body: "x", contentType: "text/plain" });
check(seen[3].init.method === "GET" && seen[4].init.method === "PATCH", "any takes the method as an option");
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("tsc"); err == nil {
		writeFile(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","strict":true,"noEmit":true,"noUnusedLocals":true,"allowImportingTsExtensions":true,"lib":["ES2022","DOM"]},"files":["types.ts","client.ts"]}`)
		tsc := exec.Command("tsc", "-p", "tsconfig.json")
		tsc.Dir = dir
		if out, err := tsc.CombinedOutput(); err != nil {
			t.Fatalf("the raw client must type check: %v\n%s", err, out)
		}
	}
}
