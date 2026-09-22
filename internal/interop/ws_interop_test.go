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
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

// runtimeSchema is the multiplexing reference case: the server answers a Run
// frame by call()ing a HostCall and awaiting the matching HostResult (a
// different oneof variant) before sending RunResult. The client then abandons
// a call of its own on a timeout; the server answers the resulting @ws_cancel
// frame with RunResult 9, proving the cancel crossed the language boundary. Every variant name is
// multi-word so a camelCase/snake_case mismatch on the wire can't hide.
const runtimeSchema = `
package wsc

message RunRequest { code: string @raw }
message Chunk {
  index: int32
  data: bytes @raw
}
message RunResult {
  exit_code: int32
  result_json: string @raw
  chunks: Chunk[]
}
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }
message Cancel { id: string @ws_id }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
    cancel: Cancel @tag("cancel") @ws_cancel
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
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"net"
	"net/http"
	"os"
	"time"

	"interop/wsc"
)

type runtimeImpl struct{}

func (runtimeImpl) Execute(ctx context.Context, req *wsc.Frame, out *wsc.RuntimeExecuteOut) error {
	if c := req.GetCancel(); c != nil && c.Id == "c-1" {
		return out.Send(ctx, &wsc.Frame{Payload: &wsc.FramePayloadRunResult{RunResult: &wsc.RunResult{ExitCode: 9}}})
	}
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
		_ = out.Send(ctx, &wsc.Frame{Payload: &wsc.FramePayloadRunResult{RunResult: runResult(req.GetRun().Code)}})
	}()
	return nil
}

func runResult(code string) *wsc.RunResult {
	return &wsc.RunResult{ExitCode: 7, ResultJson: "R:" + code, Chunks: []*wsc.Chunk{{Index: 1, Data: []byte{0, 1, 2}}, {Index: 2}, {Index: 3, Data: bytes.Repeat([]byte{0xff}, 1000)}}}
}

var bigCode = strings.Repeat("{\"k\":\"v\\n\"},", 30*1024)

func checkResult(result *wsc.RunResult) {
	if result.ExitCode != 7 {
		fail("run_result body did not decode:", fmt.Sprintf("%+v", result))
	}
	if result.ResultJson != "R:"+bigCode {
		fail("raw string did not round trip:", len(result.ResultJson))
	}
	if len(result.Chunks) != 3 || !bytes.Equal(result.Chunks[0].Data, []byte{0, 1, 2}) || len(result.Chunks[1].Data) != 0 || len(result.Chunks[2].Data) != 1000 || result.Chunks[2].Data[999] != 0xff {
		fail("raw bytes did not round trip:", fmt.Sprintf("%+v", result.Chunks))
	}
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
	run := &wsc.Frame{Payload: &wsc.FramePayloadRun{Run: &wsc.RunRequest{Code: bigCode}}}
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
			checkResult(result)
			break
		}
		fail("unexpected frame:", fmt.Sprintf("%+v", frame))
	}

	callCtx, cancelCall := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelCall()
	_, err = socket.Call(callCtx, "c-1", &wsc.Frame{Payload: &wsc.FramePayloadHostCall{HostCall: &wsc.HostCall{Id: "c-1", Method: "slow"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		fail("abandoned call: want context.DeadlineExceeded, got", err)
	}
	frame, err := socket.Receive(ctx)
	if err != nil {
		fail("receive after cancel:", err)
	}
	if result := frame.GetRunResult(); result == nil || result.ExitCode != 9 {
		fail("server never saw the cancel:", fmt.Sprintf("%+v", frame))
	}
	fmt.Println("OK")
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

function runResult(code) {
  return { exitCode: 7, resultJson: "R:" + code, chunks: [{ index: 1, data: new Uint8Array([0, 1, 2]) }, { index: 2 }, { index: 3, data: new Uint8Array(1000).fill(0xff) }] };
}

const handler = {
  async execute(req, out) {
    if (req.payload && req.payload.type === "cancel" && req.payload.cancel.id === "c-1") {
      out.send({ payload: { type: "run_result", runResult: { exitCode: 9 } } });
      return;
    }
    if (!req.payload || req.payload.type !== "run") return;
    try {
      const reply = await out.call("call-1", { payload: { type: "host_call", hostCall: { id: "call-1", method: "doThing" } } });
      const result = reply.payload && reply.payload.type === "host_result" ? reply.payload.hostResult : null;
      if (!result || result.id !== "call-1" || result.value !== "answer") {
        console.error("UNEXPECTED_REPLY", JSON.stringify(reply));
        return;
      }
      out.send({ payload: { type: "run_result", runResult: runResult(req.payload.run.code) } });
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
const { RuntimeClient, WSTimeoutError } = require("./client.js");

function fail(...args) { console.error(...args); process.exit(1); }
setTimeout(() => fail("TIMEOUT - no run_result"), 10000).unref();

const bigCode = '{"k":"v\\n"},'.repeat(30 * 1024);

function checkResult(r) {
  if (!r || r.exitCode !== 7) fail("run_result body did not decode:", JSON.stringify(r));
  if (r.resultJson !== "R:" + bigCode) fail("raw string did not round trip:", r.resultJson && r.resultJson.length);
  const c = r.chunks || [];
  const ok = c.length === 3 && c[0].data instanceof Uint8Array && c[0].data.join(",") === "0,1,2" && c[1].data.length === 0 && c[2].data.length === 1000 && c[2].data[999] === 0xff;
  if (!ok) fail("raw bytes did not round trip:", JSON.stringify(c.map((x) => [x.index, x.data && x.data.length])));
}

(async () => {
  const run = { payload: { type: "run", run: { code: bigCode } } };
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
      checkResult(payload.runResult);
      break;
    }
    fail("unexpected frame:", JSON.stringify(frame));
  }

  try {
    await socket.call("c-1", { payload: { type: "host_call", hostCall: { id: "c-1", method: "slow" } } }, { timeoutMs: 100 });
    fail("abandoned call resolved");
  } catch (err) {
    if (!(err instanceof WSTimeoutError)) fail("abandoned call: want WSTimeoutError, got", err);
  }
  const after = await socket.receive();
  if (!after.payload || after.payload.type !== "run_result" || after.payload.runResult.exitCode !== 9) {
    fail("server never saw the cancel:", JSON.stringify(after));
  }
  console.log("OK");
  socket.close();
  process.exit(0);
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

const rustCargoToml = `[package]
name = "interop"
version = "0.1.0"
edition = "2024"

[dependencies]
axum = { version = "0.8", features = ["ws"] }
base64 = "0.22"
futures-util = "0.3"
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tokio = { version = "1", features = ["full"] }
tokio-tungstenite = { version = "0.28", features = ["rustls-tls-webpki-roots"] }
urlencoding = "2"
validator = { version = "0.20", features = ["derive"] }
`

const rustHarnessMain = `#![allow(dead_code)]
mod generated;

use generated::client::RuntimeClient;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;
use std::time::Duration;

fn fail(message: String) -> ! {
    eprintln!("{message}");
    std::process::exit(1);
}

fn frame(payload: FramePayload) -> Frame {
    Frame { payload: Some(payload) }
}

fn big_code() -> String {
    "{\"k\":\"v\\n\"},".repeat(30 * 1024)
}

fn run_result(code: &str) -> RunResult {
    RunResult {
        exit_code: 7,
        result_json: format!("R:{code}"),
        chunks: vec![Chunk { index: 1, data: vec![0, 1, 2] }, Chunk { index: 2, data: vec![] }, Chunk { index: 3, data: vec![0xff; 1000] }],
    }
}

fn check_result(result: &RunResult) {
    let chunks_ok = result.chunks.len() == 3 && result.chunks[0].data == vec![0, 1, 2] && result.chunks[1].data.is_empty() && result.chunks[2].data == vec![0xff; 1000];
    if result.exit_code != 7 || result.result_json != format!("R:{}", big_code()) || !chunks_ok {
        fail(format!("raw round trip failed: exit {} len {}", result.exit_code, result.result_json.len()));
    }
}

struct Impl;

impl Runtime for Impl {
    fn execute(&self, _context: RequestContext, req: Frame, out: WsCallSink<String, Frame, Frame>) -> impl std::future::Future<Output = Result<(), RuntimeExecuteServerError>> + Send {
        async move {
            match req.payload {
                Some(FramePayload::Cancel(cancel)) if cancel.id == "c-1" => {
                    let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 9, ..Default::default() }))).await;
                }
                Some(FramePayload::Run(run)) => {
                    tokio::spawn(async move {
                        let call = frame(FramePayload::HostCall(HostCall { id: "call-1".into(), method: "doThing".into() }));
                        match out.call("call-1".to_string(), call).await {
                            Ok(Frame { payload: Some(FramePayload::HostResult(result)) }) if result.value == "answer" => {
                                let _ = out.send(frame(FramePayload::RunResult(run_result(&run.code)))).await;
                            }
                            other => eprintln!("UNEXPECTED_REPLY {other:?}"),
                        }
                    });
                }
                _ => {}
            }
            Ok(())
        }
    }
}

async fn serve() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    println!("PORT={}", listener.local_addr().expect("addr").port());
    axum::serve(listener, runtime_router(Arc::new(Impl))).await.expect("serve");
}

async fn client(base: String) {
    let outcome = tokio::time::timeout(Duration::from_secs(10), async {
        let run = frame(FramePayload::Run(RunRequest { code: big_code() }));
        let socket = RuntimeClient::new(base).execute(&run).await.expect("connect");
        socket.send(&run).await.expect("send run");
        loop {
            match socket.receive().await.and_then(|f| f.payload) {
                Some(FramePayload::HostCall(call)) => {
                    if call.method != "doThing" { fail(format!("host_call body did not decode: {call:?}")); }
                    let reply = frame(FramePayload::HostResult(HostResult { id: call.id, value: "answer".into() }));
                    socket.send(&reply).await.expect("send host result");
                }
                Some(FramePayload::RunResult(result)) => {
                    check_result(&result);
                    break;
                }
                other => fail(format!("unexpected frame {other:?}")),
            }
        }
        let call = frame(FramePayload::HostCall(HostCall { id: "c-1".into(), method: "slow".into() }));
        match socket.call_timeout("c-1".to_string(), &call, Duration::from_millis(100)).await {
            Err(generated::client::WsCallError::TimedOut) => {}
            other => fail(format!("abandoned call: want TimedOut, got {other:?}")),
        }
        match socket.receive().await.and_then(|f| f.payload) {
            Some(FramePayload::RunResult(result)) if result.exit_code == 9 => {}
            other => fail(format!("server never saw the cancel: {other:?}")),
        }
    })
    .await;
    if outcome.is_err() {
        fail("TIMEOUT".to_string());
    }
    println!("OK");
}

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    match args.get(1).map(String::as_str) {
        Some("server") => serve().await,
        Some("client") => client(args.get(2).cloned().expect("base url")).await,
        _ => fail("usage: interop server | interop client <base>".to_string()),
    }
}
`

func TestWSGoClientRustServer(t *testing.T) {
	goBin := buildGoHarness(t)
	rustBin := buildRustHarness(t)
	port := startServer(t, "", rustBin, "server")
	expectOK(t, "", goBin, "client", "http://127.0.0.1:"+port)
}

func TestWSRustClientGoServer(t *testing.T) {
	goBin := buildGoHarness(t)
	rustBin := buildRustHarness(t)
	port := startServer(t, "", goBin, "server")
	expectOK(t, "", rustBin, "client", "http://127.0.0.1:"+port)
}

func buildRustHarness(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	file := compileSchema(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), rustCargoToml)
	writeFile(t, filepath.Join(dir, "src", "main.rs"), rustHarnessMain)
	writeFile(t, filepath.Join(dir, "src", "generated", "mod.rs"), "pub mod types;\npub mod server;\npub mod client;\n")
	writeFile(t, filepath.Join(dir, "src", "generated", "types.rs"), string(genrust.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "src", "generated", "server.rs"), string(genrust.GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "src", "generated", "client.rs"), string(genrust.GenerateClient(file)))
	run(t, dir, "cargo", "build", "--quiet")
	bin := filepath.Join(dir, "target", "debug", "interop")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}
