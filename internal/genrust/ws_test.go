package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const rustWSFixture = `
package rwf

message ChatMessage { room: string text: string }
message ChatEvent { seq: int64 }

service ChatService {
  base_path: "/v1"

  chat(ChatMessage) -> ChatEvent @ws("/rooms/{room}")
}
`

func TestGenerateRustWS(t *testing.T) {
	ast, err := onklang.Parse(rustWSFixture)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "ws.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	file := pkg.Files[0]

	server := GenerateServer(file)
	for _, want := range []string{
		"pub struct WsSink<E> {",
		"fn chat(&self, context: RequestContext, req: ChatMessage, out: WsSink<ChatEvent>) -> impl std::future::Future<Output = Result<(), ChatServiceChatServerError>> + Send;",
		`axum::routing::get(chat_service_chat_ws_handler::<T>)`,
		"axum::extract::ws::WebSocketUpgrade",
		"while let Some(message) = stream.next().await {",
		"service.chat(context.clone(), frame, out.clone()).await",
	} {
		if !strings.Contains(string(server), want) {
			t.Fatalf("generated rust server missing %q:\n%s", want, server)
		}
	}

	client := GenerateClient(file)
	for _, want := range []string{
		"pub struct WsFrameSocket<In, Out> {",
		"tokio_tungstenite::connect_async(url)",
		"pub async fn chat(&self, req: &ChatMessage) -> Result<WsFrameSocket<ChatMessage, ChatEvent>, ChatServiceChatError> {",
		"Ok(WsFrameSocket::<ChatMessage, ChatEvent>::new(stream))",
	} {
		if !strings.Contains(string(client), want) {
			t.Fatalf("generated rust client missing %q:\n%s", want, client)
		}
	}
}

const rustWSCorrelatedFixture = `
package rwc

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

func compileRustWSCorrelatedFile(t *testing.T) *onkir.Package {
	t.Helper()
	ast, err := onklang.Parse(rustWSCorrelatedFixture)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "wsc.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg
}

func TestGenerateRustWSCorrelated(t *testing.T) {
	pkg := compileRustWSCorrelatedFile(t)
	file := pkg.Files[0]

	types := GenerateTypes(file)
	for _, want := range []string{
		"pub trait WsCorrelated<K> {",
		"impl WsCorrelated<String> for Frame {",
		// Both oneof variants carrying @ws_id must get a match arm, not
		// just whichever one happens to be first by declaration order.
		`if let Some(FramePayload::HostCall(value)) = &self.payload {`,
		`if let Some(FramePayload::HostResult(value)) = &self.payload {`,
	} {
		if !strings.Contains(string(types), want) {
			t.Fatalf("generated rust types missing %q:\n%s", want, types)
		}
	}

	server := GenerateServer(file)
	for _, want := range []string{
		"pub struct WsPending<K, T> {",
		"pub struct WsCallSink<K, E, R> {",
		"fn execute(&self, context: RequestContext, req: Frame, out: WsCallSink<String, Frame, Frame>)",
		"let frame = match frame.ws_id() {",
		"Some(id) => match out.pending.resolve(&id, frame).await { Some(frame) => frame, None => continue },",
		"out.pending.close_all().await;",
	} {
		if !strings.Contains(string(server), want) {
			t.Fatalf("generated correlated rust server missing %q:\n%s", want, server)
		}
	}

	client := GenerateClient(file)
	for _, want := range []string{
		"pub struct WsPending<K, T> {",
		"pub struct WsCallSocket<K, In, Out> {",
		"pub async fn execute(&self, req: &Frame) -> Result<WsCallSocket<String, Frame, Frame>, RuntimeExecuteError> {",
		"pub async fn call(&self, id: K, value: &In) -> Result<Out, String> {",
	} {
		if !strings.Contains(string(client), want) {
			t.Fatalf("generated correlated rust client missing %q:\n%s", want, client)
		}
	}
}

const rustWSCargoToml = `
[package]
name = "onekit-rust-ws-fixture"
version = "0.1.0"
edition = "2024"

[dependencies]
axum = { version = "0.8", features = ["ws"] }
futures-util = "0.3"
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tokio = { version = "1", features = ["full"] }
tokio-tungstenite = { version = "0.28", features = ["rustls-tls-webpki-roots"] }
urlencoding = "2"
validator = "0.20"
`

// TestGeneratedRustWSCompiles pins that a plain (no @ws_id) @ws server AND
// client actually build against real crates - substring assertions alone
// already missed a pre-existing bug (a nested struct/impl inside another
// impl block, and a client return type that referenced a type nothing
// defined) that made this codegen non-compiling from the start.
func TestGeneratedRustWSCompiles(t *testing.T) {
	runRustWSCrate(t, rustWSFixture, "onekit-rust-ws-fixture-plain", rustBuildOnlyMain, false)
}

// TestGeneratedRustWSCorrelatedCompiles is the same check for the @ws_id
// correlation path (WsPending/WsCallSink/WsCallSocket, the restructured
// multi-frame read loop, and the WsCorrelated trait impls in types.rs).
func TestGeneratedRustWSCorrelatedCompiles(t *testing.T) {
	runRustWSCrate(t, rustWSCorrelatedFixture, "onekit-rust-ws-fixture-correlated", rustBuildOnlyMain, false)
}

const rustBuildOnlyMain = "mod generated;\nfn main() {}\n"

// rustWSCorrelatedRuntimeMain drives a generated axum server and the generated
// client against each other through the multi-variant correlated round trip
// the Go and TS runtime tests use: the server answers "run" by call()ing a
// HostCall and awaiting the matching HostResult (a different oneof variant)
// before sending RunResult. It also pins a client-side bug this test was
// written to catch: the background reader used to drop any inbound frame that
// carried an @ws_id but wasn't a reply to a pending call - which is exactly
// what a server-pushed HostCall is - so receive() never saw it.
const rustWSCorrelatedRuntimeMain = `#![allow(dead_code)]
mod generated;

use generated::client::*;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;
use std::time::Duration;

fn frame(payload: FramePayload) -> Frame {
    Frame { payload: Some(payload) }
}

struct Impl;

impl Runtime for Impl {
    fn execute(&self, _context: RequestContext, req: Frame, out: WsCallSink<String, Frame, Frame>) -> impl std::future::Future<Output = Result<(), RuntimeExecuteServerError>> + Send {
        async move {
            if !matches!(req.payload, Some(FramePayload::Run(_))) {
                return Ok(());
            }
            tokio::spawn(async move {
                let call = frame(FramePayload::HostCall(HostCall { id: "call-1".into(), method: "doThing".into() }));
                match out.call("call-1".to_string(), call).await {
                    Ok(Frame { payload: Some(FramePayload::HostResult(result)) }) if result.id == "call-1" && result.value == "answer" => {
                        let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 0 }))).await;
                    }
                    other => {
                        eprintln!("UNEXPECTED_REPLY {other:?}");
                        std::process::exit(1);
                    }
                }
            });
            Ok(())
        }
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let addr = listener.local_addr().expect("local addr");
    tokio::spawn(async move {
        axum::serve(listener, runtime_router(Arc::new(Impl))).await.expect("serve");
    });

    let client = RuntimeClient::new(format!("http://{addr}"));
    let run = frame(FramePayload::Run(RunRequest { code: "x".into() }));
    let socket = client.execute(&run).await.expect("connect");
    socket.send(&run).await.expect("send run");

    let outcome = tokio::time::timeout(Duration::from_secs(10), async {
        while let Some(received) = socket.receive().await {
            match received.payload {
                Some(FramePayload::HostCall(call)) => {
                    let reply = frame(FramePayload::HostResult(HostResult { id: call.id, value: "answer".into() }));
                    socket.send(&reply).await.expect("send host result");
                }
                Some(FramePayload::RunResult(_)) => return Ok(()),
                other => return Err(format!("unexpected frame {other:?}")),
            }
        }
        Err("connection closed before RunResult".to_string())
    })
    .await;

    match outcome {
        Ok(Ok(())) => println!("OK"),
        Ok(Err(error)) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
        Err(_) => {
            eprintln!("TIMEOUT - a server-pushed HostCall never reached receive(), or Call() never got its HostResult");
            std::process::exit(1);
        }
    }
}
`

func TestGeneratedRustWSCorrelatedRuntimeRoutesMultipleVariants(t *testing.T) {
	runRustWSCrate(t, rustWSCorrelatedFixture, "onekit-rust-ws-fixture-runtime", rustWSCorrelatedRuntimeMain, true)
}

// runRustWSCrate generates types/server/client for src into a scratch crate
// with mainRS as src/main.rs, and builds it - then, when run is true,
// executes it and requires it to print "OK".
func runRustWSCrate(t *testing.T, src, crateName, mainRS string, run bool) {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "ws.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	file := pkg.Files[0]

	types := GenerateTypes(file)
	server := GenerateServer(file)
	client := GenerateClient(file)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "generated"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cargoToml := strings.Replace(rustWSCargoToml, "onekit-rust-ws-fixture", crateName, 1)
	files := map[string]string{
		"Cargo.toml":              cargoToml,
		"src/main.rs":             mainRS,
		"src/generated/mod.rs":    "pub mod types;\npub mod server;\npub mod client;\n",
		"src/generated/types.rs":  string(types),
		"src/generated/server.rs": string(server),
		"src/generated/client.rs": string(client),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	build := exec.Command("cargo", "build", "--quiet")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust WS code failed to build: %v\n%s\n--- types.rs ---\n%s\n--- server.rs ---\n%s\n--- client.rs ---\n%s",
			err, out, types, server, client)
	}
	if !run {
		return
	}
	runCmd := exec.Command("cargo", "run", "--quiet")
	runCmd.Dir = dir
	var stderr strings.Builder
	runCmd.Stderr = &stderr
	out, err := runCmd.Output()
	if err != nil {
		t.Fatalf("generated Rust WS runtime harness failed: %v\n%s%s", err, out, stderr.String())
	}
	if got := strings.TrimSpace(string(out)); got != "OK" {
		t.Fatalf("expected OK, got %q", got)
	}
}
