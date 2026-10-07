package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const rawHTTPSchema = `package app

message Empty {}
message Bundle { slug: string  v: int32 @query }
message Upload { slug: string  key: string }

service Apps {
  base_path: "/apps"
  bundle(Bundle) -> Empty @get("/{slug}/bundle.js") @http("application/javascript")
  put_object(Upload) -> Empty @put("/{slug}/objects/{key...}") @http
}
`

func TestTSRawHTTPClientReturnsTheResponse(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onklang.Parse(rawHTTPSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client"}})
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
