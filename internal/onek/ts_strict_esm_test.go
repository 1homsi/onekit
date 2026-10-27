package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const strictESMToml = `
module = "example.com/strict/gen/go"

[generate.ts-client]
out = "./gen/client"
msw = true

[generate.ts-server]
out = "./gen/server"
`

const strictESMRuntimeTypeSpec = `namespace rt;

model HostCall {
  @Onkt.wsId
  id: string;
  method: string;
}

model HostResult {
  @Onkt.wsId
  id: string;
  value?: string;
}

model Cancel {
  @Onkt.wsId
  id: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("host_call") host_call: HostCall,
  @Onkt.tag("host_result") host_result: HostResult,
  @Onkt.tag("cancel") @Onkt.wsCancel cancel: Cancel,
}

model Frame {
  payload: FramePayload;
}

model Tree {
  name: string;
  children: Tree[];
  meta: Meta;
}

model Meta {
  note: string;
}

@route("/v1")
interface Runtime {
  @Onkt.ws
  @route("/execute")
  execute(...Frame): Frame;

  @get
  @route("/tree")
  tree(...Meta): Tree;
}
`

const strictESMSharedTypeSpec = `namespace common;

model Lookup {
  id: string;
}

model Tick {
  id: string;
  at: utcDateTime;
}

@error
model NotFound {
  @statusCode _: 404;
  code: string;
}
`

const strictESMCatalogTypeSpec = `using common;

namespace hub.catalog;

@route("/catalog/v1")
interface CatalogService {
  @get
  @route("/prices/{id}")
  getPrice(...Lookup): Money | NotFound;

  @post
  @route("/prices")
  setPrice(...Money): Money | NotFound;

  @get
  @route("/prices/{id}/watch")
  @Onkt.stream
  watchPrice(...Lookup): Tick | NotFound;

  @Onkt.ws
  @route("/live")
  live(...Lookup): Tick;
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
// (types, client, server, msw; single-package, cross-package
// and @ws) with the settings a strict modern Node project uses, then loads
// each module in Node ESM.
func TestBuildTSCompilesUnderStrictNodeESM(t *testing.T) {
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), strictESMToml)
	// The example's own schema, under api/ beside the other packages.
	for name, target := range map[string]string{"models.tsp": "models.tsp", "main.tsp": "service.tsp"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "examples", "simple-api", name))
		if err != nil {
			t.Fatalf("read example schema: %v", err)
		}
		writeTestFile(t, filepath.Join(dir, "api", target), string(src))
	}
	writeTestFile(t, filepath.Join(dir, "common", "money.tsp"), commonMoneyTypeSpec)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.tsp"), businessServiceTypeSpec)
	writeTestFile(t, filepath.Join(dir, "rt", "runtime.tsp"), strictESMRuntimeTypeSpec)
	writeTestFile(t, filepath.Join(dir, "common", "shared.tsp"), strictESMSharedTypeSpec)
	writeTestFile(t, filepath.Join(dir, "hub", "catalog", "v1", "service.tsp"), strictESMCatalogTypeSpec)
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
    "erasableSyntaxOnly": true,
    "verbatimModuleSyntax": true,
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
		"ws", "@types/ws", "@types/node", "msw")
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
