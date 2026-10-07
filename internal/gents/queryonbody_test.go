package gents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const queryOnBodySchema = `package app

message Save {
  id: string
  name: string
  dry_run: bool @query
  tags: string[] @query("tag")
}
message Saved { id: string  name: string  dry_run: bool  tags: string[] }

service Things {
  base_path: "/v1"
  save(Save) -> Saved @put("/things/{id}")
}
`

func TestTSQueryParamsOnBodyBearingRoutes(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	ast, err := onklang.Parse(queryOnBodySchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.onk", AST: ast}}, onkcompile.CompileOptions{Targets: []string{"ts-client", "ts-server"}})
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
	writeFile(t, filepath.Join(dir, "server.ts"), forNode(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { ThingsClient } from "./client.ts";
import { createThingsFetchHandler } from "./server.ts";

const handle = createThingsFetchHandler({
  save: async (req: any) => ({ id: req.id, name: req.name, dryRun: req.dryRun, tags: req.tags ?? [] }),
} as any);
let seenUrl = "";
const client = new ThingsClient("http://api", {
  fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
    seenUrl = String(input);
    return handle(new Request(String(input), init));
  }) as typeof fetch,
});
const out = await client.save({ id: "7", name: "n", dryRun: true, tags: ["a", "b"] });
if (!seenUrl.includes("dry_run=true") || !seenUrl.includes("tag=a") || !seenUrl.includes("tag=b")) throw new Error("query params missing: " + seenUrl);
if (out.dryRun !== true || out.tags.join() !== "a,b" || out.name !== "n" || out.id !== "7") throw new Error("round trip failed: " + JSON.stringify(out));
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	_ = os.Remove(filepath.Join(dir, "main.ts"))
}
