// Package interop holds cross-language wire tests: code generated for one
// target talking to code generated for another over a real connection. The
// per-generator tests only ever pair a target with itself, which is how a TS
// server that put its in-memory (camelCase) shape on the wire shipped - its
// own TS client never noticed, but a Go client decoded every body as nil.
package interop

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

// runtimeSchema is the multiplexing reference case: the server answers a Run
// frame by call()ing a HostCall and awaiting the matching HostResult (a
// different oneof variant) before sending RunResult. Every variant name is
// multi-word so a camelCase/snake_case mismatch on the wire can't hide.
const runtimeSchema = `
package wsc

message RunRequest { code: string }
message RunResult { exit_code: int32 }
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
  }
}

service Runtime {
  base_path: "/v1"

  execute(Frame) -> Frame @ws("/execute")
}
`

// The harnesses send RunResult with exit code 7, not 0, so a body that fails
// to decode (and silently takes its zero value) is caught.

// goHarnessMain is a small program over the generated Go package: "server"
// serves the Runtime service and prints PORT=<n>; "client <baseURL>" drives
// the round trip against any server and exits 0 only if it completes.
const goHarnessMain = `package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"interop/wsc"
)

type runtimeImpl struct{}

func (runtimeImpl) Execute(ctx context.Context, req *wsc.Frame, out *wsc.RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		reply, err := out.Call(ctx, "call-1", &wsc.Frame{Payload: &wsc.FramePayloadHostCall{HostCall: &wsc.HostCall{Id: "call-1", Method: "doThing"}}})
		if err != nil {
			fmt.Fprintln(os.Stderr, "CALL_ERROR", err)
			return
		}
		result := reply.GetHostResult()
		if result == nil || result.Id != "call-1" || result.Value != "answer" {
			fmt.Fprintf(os.Stderr, "UNEXPECTED_REPLY %+v\n", reply)
			return
		}
		_ = out.Send(ctx, &wsc.Frame{Payload: &wsc.FramePayloadRunResult{RunResult: &wsc.RunResult{ExitCode: 7}}})
	}()
	return nil
}

func serve() {
	mux := http.NewServeMux()
	if err := wsc.RegisterRuntimeServer(mux, runtimeImpl{}); err != nil {
		fail("register:", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail("listen:", err)
	}
	fmt.Printf("PORT=%d\n", listener.Addr().(*net.TCPAddr).Port)
	fail("serve:", http.Serve(listener, mux))
}

func client(baseURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := &wsc.Frame{Payload: &wsc.FramePayloadRun{Run: &wsc.RunRequest{Code: "print(1)"}}}
	socket, err := wsc.NewRuntimeClient(baseURL).Execute(ctx, run)
	if err != nil {
		fail("connect:", err)
	}
	defer socket.Close()
	if err := socket.Send(ctx, run); err != nil {
		fail("send run:", err)
	}
	for {
		frame, err := socket.Receive(ctx)
		if err != nil {
			fail("receive:", err)
		}
		if call := frame.GetHostCall(); call != nil {
			if call.Id != "call-1" || call.Method != "doThing" {
				fail("host_call body did not decode:", fmt.Sprintf("%+v", call))
			}
			reply := &wsc.Frame{Payload: &wsc.FramePayloadHostResult{HostResult: &wsc.HostResult{Id: call.Id, Value: "answer"}}}
			if err := socket.Send(ctx, reply); err != nil {
				fail("send host_result:", err)
			}
			continue
		}
		if result := frame.GetRunResult(); result != nil {
			if result.ExitCode != 7 {
				fail("run_result body did not decode:", fmt.Sprintf("%+v", result))
			}
			fmt.Println("OK")
			return
		}
		fail("unexpected frame:", fmt.Sprintf("%+v", frame))
	}
}

func fail(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func main() {
	switch {
	case len(os.Args) == 2 && os.Args[1] == "server":
		serve()
	case len(os.Args) == 3 && os.Args[1] == "client":
		client(os.Args[2])
	default:
		fail("usage: interop server | interop client <baseURL>")
	}
}
`

// tsServerHarness serves the generated Node adapter and prints PORT=<n>. The
// handler works in the typed TS shape; server.ts owns encoding to the wire.
const tsServerHarness = `"use strict";
const http = require("node:http");
const { attachRuntimeNodeSocketHandlers } = require("./server.js");

const handler = {
  async execute(req, out) {
    if (!req.payload || req.payload.type !== "run") return;
    try {
      const reply = await out.call("call-1", { payload: { type: "host_call", hostCall: { id: "call-1", method: "doThing" } } });
      const result = reply.payload && reply.payload.type === "host_result" ? reply.payload.hostResult : null;
      if (!result || result.id !== "call-1" || result.value !== "answer") {
        console.error("UNEXPECTED_REPLY", JSON.stringify(reply));
        return;
      }
      out.send({ payload: { type: "run_result", runResult: { exitCode: 7 } } });
    } catch (err) {
      console.error("CALL_ERROR", err);
    }
  },
};

const httpServer = http.createServer((req, res) => { res.writeHead(404); res.end(); });
attachRuntimeNodeSocketHandlers(httpServer, handler);
httpServer.listen(0, "127.0.0.1", () => console.log("PORT=" + httpServer.address().port));
`

// tsClientHarness drives the generated TS client (on Node's global
// WebSocket) against the server at argv[2].
const tsClientHarness = `"use strict";
const { RuntimeClient } = require("./client.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT - no run_result"), 10000).unref();

(async () => {
  const run = { payload: { type: "run", run: { code: "print(1)" } } };
  const socket = await new RuntimeClient(process.argv[2]).execute(run);
  socket.send(run);
  for (;;) {
    const frame = await socket.receive();
    const payload = frame.payload;
    if (payload && payload.type === "host_call") {
      if (!payload.hostCall || payload.hostCall.id !== "call-1" || payload.hostCall.method !== "doThing") {
        fail("host_call body did not decode:", JSON.stringify(frame));
      }
      socket.send({ payload: { type: "host_result", hostResult: { id: payload.hostCall.id, value: "answer" } } });
      continue;
    }
    if (payload && payload.type === "run_result") {
      if (!payload.runResult || payload.runResult.exitCode !== 7) {
        fail("run_result body did not decode:", JSON.stringify(frame));
      }
      console.log("OK");
      socket.close();
      process.exit(0);
    }
    fail("unexpected frame:", JSON.stringify(frame));
  }
})().catch((err) => fail("CLIENT_ERROR", err));
`

func TestWSGoClientTSServer(t *testing.T) {
	goBin := buildGoHarness(t)
	tsDir := buildTSHarness(t)
	port := startServer(t, tsDir, "node", "ts-server.js")
	expectOK(t, "", goBin, "client", "http://127.0.0.1:"+port)
}

func TestWSTSClientGoServer(t *testing.T) {
	goBin := buildGoHarness(t)
	tsDir := buildTSHarness(t)
	port := startServer(t, "", goBin, "server")
	expectOK(t, tsDir, "node", "ts-client.js", "http://127.0.0.1:"+port)
}

func compileSchema(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(runtimeSchema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "wsc.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

// buildGoHarness generates the Go target into a scratch module and builds
// goHarnessMain against it, returning the binary path.
func buildGoHarness(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileSchema(t)
	dir := t.TempDir()
	generated := map[string]func(*onkir.File) ([]byte, error){
		"wsc/server.go":       gengo.GenerateServer,
		"wsc/client.go":       gengo.GenerateClient,
		"wsc/types.gen.go":    gengo.GenerateTypes,
		"wsc/validate.gen.go": gengo.GenerateValidation,
	}
	for name, generate := range generated {
		src, err := generate(file)
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		writeFile(t, filepath.Join(dir, name), string(src))
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module interop\n\ngo 1.24\n\nrequire github.com/coder/websocket v1.8.15\n")
	writeFile(t, filepath.Join(dir, "main.go"), goHarnessMain)
	run(t, dir, "go", "mod", "tidy")
	bin := filepath.Join(dir, "interop")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(t, dir, "go", "build", "-o", bin, ".")
	return bin
}

// buildTSHarness generates the TS target, installs the Node adapter's peer
// dependencies, and compiles everything to CommonJS next to the harnesses.
func buildTSHarness(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	file := compileSchema(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(gents.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(gents.GenerateServerWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(gents.GenerateClientWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "ts-server.js"), tsServerHarness)
	writeFile(t, filepath.Join(dir, "ts-client.js"), tsClientHarness)
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "ws-interop", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "node16",
    "moduleResolution": "node16",
    "esModuleInterop": true,
    "strict": true,
    "types": ["node", "ws"],
    "lib": ["ES2022", "DOM"]
  },
  "files": ["types.ts", "server.ts", "client.ts"]
}
`)
	run(t, dir, "npm", "install", "--no-audit", "--no-fund", "ws", "@types/node", "@types/ws")
	run(t, dir, "tsc", "-p", "tsconfig.json")
	return dir
}

// startServer launches a harness server, waits for its PORT=<n> line, and
// kills it when the test ends. Its stderr is surfaced on failure.
func startServer(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})

	ports := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if port, ok := strings.CutPrefix(scanner.Text(), "PORT="); ok {
				ports <- port
				break
			}
		}
		// A scan error just means no port line; the select below times out.
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case port := <-ports:
		return port
	case <-time.After(30 * time.Second):
		t.Fatalf("%s never reported a port", name)
		return ""
	}
}

// expectOK runs a harness client and requires it to print exactly "OK".
func expectOK(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("client failed: %v\nstdout: %s\nstderr: %s", err, out, stderr.String())
	}
	if got := strings.TrimSpace(string(out)); got != "OK" {
		t.Fatalf("expected OK, got %q\nstderr: %s", got, stderr.String())
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
