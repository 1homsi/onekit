package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const strictESMOnekitToml = `
module = "example.com/strict/gen/go"

[generate.ts-client]
out = "./gen/client"
zod = true
react_query = true
msw = true

[generate.ts-server]
out = "./gen/server"
`

const strictESMRuntimeOnk = `
package rt

message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string? }
message Cancel { id: string @ws_id }

message Frame {
  payload: oneof(discriminator: "type") {
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    cancel: Cancel @tag("cancel") @ws_cancel
  }
}

message Tree {
  name: string
  children: Tree[]
  meta: Meta
}

message Meta { note: string }

service Runtime {
  base_path: "/v1"

  execute(Frame) -> Frame @ws("/execute")
  tree(Meta) -> Tree @get("/tree")
}
`

// strictESMHarness imports every compiled module in real Node ESM, which
// resolves relative specifiers exactly as written - an extensionless
// "./types" fails here even when a bundler or tsc would have accepted it.
const strictESMHarness = `
import { readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : path.endsWith(".js") ? [path] : [];
  });
}

const files = walk("dist");
if (files.length === 0) throw new Error("no compiled modules");
for (const file of files) await import(pathToFileURL(file).href);
console.log("OK " + files.length);
`

// TestBuildTSCompilesUnderStrictNodeESM compiles every generated TS output
// (types, client, server, zod, react-query, msw; single-package, cross-package
// and @ws) with the settings a strict modern Node project uses, then loads
// each module in Node ESM.
func TestBuildTSCompilesUnderStrictNodeESM(t *testing.T) {
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), strictESMOnekitToml)
	for _, name := range []string{"models.onk", "service.onk"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "examples", "onk-simple-api", name))
		if err != nil {
			t.Fatalf("read example schema: %v", err)
		}
		writeTestFile(t, filepath.Join(dir, "api", name), string(src))
	}
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), businessServiceOnk)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.onk"), strictESMRuntimeOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	gen := filepath.Join(dir, "gen")
	writeTestFile(t, filepath.Join(gen, "package.json"), `{"name": "strict-esm", "private": true, "type": "module"}`)
	writeTestFile(t, filepath.Join(gen, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "lib": ["ES2022", "DOM"],
    "types": ["node", "ws"],
    "jsx": "react-jsx",
    "skipLibCheck": true,
    "rootDir": ".",
    "outDir": "dist"
  },
  "include": ["client/**/*.ts", "server/**/*.ts"]
}
`)
	writeTestFile(t, filepath.Join(gen, "check.mjs"), strictESMHarness)
	runIn(t, gen, "npm", "install", "--no-audit", "--no-fund",
		"ws", "@types/ws", "@types/node", "zod", "react", "@types/react", "@tanstack/react-query", "msw")
	runIn(t, gen, "tsc", "-p", "tsconfig.json")
	out := runIn(t, gen, "node", "check.mjs")
	if !strings.HasPrefix(strings.TrimSpace(out), "OK ") {
		t.Fatalf("ESM import check: %s", out)
	}
}

func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
