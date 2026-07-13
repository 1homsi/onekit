package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const sseFixtureSrc = `
package app

message StreamEventsRequest {
  channel: string @query("channel")
}

message Event {
  id: string
  payload: string
}

message StreamError @status(400) {
  reason: string
}

service SSEService {
  base_path: "/api/v1"

  streamEvents(StreamEventsRequest) -> Event | StreamError @get("/events") @stream
}
`

func compileSSEFixture(t *testing.T) *onkcompile.Source {
	t.Helper()
	ast, err := onklang.Parse(sseFixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return &onkcompile.Source{Path: "app.onk", AST: ast}
}

const sseHarness = `
import * as http from "node:http";
import { createSSEServiceRoutes, HttpError } from "./server.ts";
import type { RouteDescriptor } from "./server.ts";
import type { StreamEventsRequest, Event, StreamError } from "./types.ts";
import { SSEServiceClient } from "./client.ts";

const handler = {
  streamEvents(req: StreamEventsRequest): ReadableStream<Event> {
    return new ReadableStream<Event>({
      async start(controller) {
        if (req.channel === "fail-early") {
          throw new HttpError(400, { reason: "early failure" } satisfies StreamError);
        }
        if (req.channel === "fail-late") {
          controller.enqueue({ id: "1", payload: "first" });
          throw new Error("boom");
        }
        for (let i = 1; i <= 3; i++) {
          controller.enqueue({ id: String(i), payload: "hello" });
        }
        controller.close();
      },
    });
  },
};

function findRoute(routes: RouteDescriptor[], method: string, path: string): RouteDescriptor {
  const r = routes.find((r) => r.method === method && r.path === path);
  if (!r) throw new Error("route not found: " + method + " " + path);
  return r;
}

async function main() {
  const routes = createSSEServiceRoutes(handler);
  const route = findRoute(routes, "GET", "/api/v1/events");

  const server = http.createServer((nodeReq, nodeRes) => {
    void (async () => {
      const url = "http://localhost" + nodeReq.url;
      const webReq = new Request(url, { method: nodeReq.method });
      const webRes = await route.handler(webReq);
      nodeRes.writeHead(webRes.status, Object.fromEntries(webRes.headers));
      if (!webRes.body) {
        nodeRes.end();
        return;
      }
      const reader = webRes.body.getReader();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        nodeRes.write(value);
      }
      nodeRes.end();
    })();
  });

  await new Promise<void>((resolve) => server.listen(0, resolve));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  const client = new SSEServiceClient("http://localhost:" + port);

  const got: string[] = [];
  for await (const event of client.streamEvents({ channel: "ok" })) {
    got.push(event.id + ":" + event.payload);
  }
  if (got.length !== 3 || got[0] !== "1:hello" || got[2] !== "3:hello") {
    throw new Error("unexpected events: " + JSON.stringify(got));
  }

  let earlyErr: unknown;
  try {
    for await (const _event of client.streamEvents({ channel: "fail-early" })) {
      // noop
    }
  } catch (err) {
    earlyErr = err;
  }
  const streamErr = earlyErr as StreamError;
  if (!streamErr || streamErr.reason !== "early failure") {
    throw new Error("unexpected early error: " + JSON.stringify(earlyErr));
  }

  const lateGot: string[] = [];
  let lateErr: unknown;
  try {
    for await (const event of client.streamEvents({ channel: "fail-late" })) {
      lateGot.push(event.id + ":" + event.payload);
    }
  } catch (err) {
    lateErr = err;
  }
  if (!lateErr) {
    throw new Error("expected late stream error");
  }
  if (lateGot.length !== 1 || lateGot[0] !== "1:first") {
    throw new Error("unexpected late events: " + JSON.stringify(lateGot));
  }

  server.close();
  console.log("OK");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`

func TestSSEStreamingTypeScriptTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}

	src := compileSSEFixture(t)
	pkg, err := onkcompile.Compile([]onkcompile.Source{*src})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClient(file)
	serverSrc := GenerateServer(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "client.ts"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "server.ts"), string(serverSrc))
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc type check failed: %v\n%s", err, out)
	}
}

func TestSSEStreamingRuntimeBehavior(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	src := compileSSEFixture(t)
	pkg, err := onkcompile.Compile([]onkcompile.Source{*src})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClient(file)
	serverSrc := GenerateServer(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	// Real consumers resolve the generated extensionless "./types" import
	// through a bundler (webpack/vite/esbuild all handle this fine, and it's
	// what tsc's own "bundler" moduleResolution mode - used by the sibling
	// type-check test above - expects). Node's native TS execution used only
	// by *this* runtime test needs an explicit extension, same as this
	// harness's own imports below already use - so patch just the copy
	// written to disk here, not the generator's real output.
	clientSrcForNode := strings.ReplaceAll(string(clientSrc), `from "./types"`, `from "./types.ts"`)
	writeFile(t, filepath.Join(dir, "client.ts"), clientSrcForNode)
	serverSrcForNode := strings.ReplaceAll(string(serverSrc), `from "./types"`, `from "./types.ts"`)
	writeFile(t, filepath.Join(dir, "server.ts"), serverSrcForNode)
	writeFile(t, filepath.Join(dir, "main.ts"), sseHarness)

	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
