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
		"message = stream.next() => Some(message),",
		"service.chat(context.clone(), frame, out.clone()).await",
	} {
		if !strings.Contains(string(server), want) {
			t.Fatalf("generated rust server missing %q:\n%s", want, server)
		}
	}

	client := GenerateClient(file)
	for _, want := range []string{
		"pub struct WsFrameSocket<In, Out> {",
		"tokio_tungstenite::connect_async_with_config(url, Some(config), false)",
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
		"Some(id) => { let variant = frame.ws_variant(); match out.pending.resolve(&id, variant, frame).await { Some(frame) => frame, None => continue } }",
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
		"pub async fn call(&self, id: K, value: &In) -> Result<Out, WsCallError> {",
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
// the Go and TS runtime tests use, then call_timeout plus the @ws_cancel frame
// in each direction: the server answers "run" by call()ing a
// HostCall and awaiting the matching HostResult (a different oneof variant)
// before sending RunResult. It also pins a client-side bug this test was
// written to catch: the background reader used to drop any inbound frame that
// carried an @ws_id but wasn't a reply to a pending call - which is exactly
// what a server-pushed HostCall is - so receive() never saw it.
const rustWSCorrelatedRuntimeMain = `#![allow(dead_code)]
mod generated;

use generated::client::{RuntimeClient, WsCallSocket};
use generated::server::*;
use generated::types::*;
use std::sync::Arc;
use std::time::Duration;

fn frame(payload: FramePayload) -> Frame {
    Frame { payload: Some(payload) }
}

fn host_call(id: &str) -> Frame {
    frame(FramePayload::HostCall(HostCall { id: id.into(), method: "doThing".into() }))
}

fn fail(message: String) -> ! {
    eprintln!("{message}");
    std::process::exit(1);
}

struct Impl;

static WATCHED_CLOSE: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);

impl Runtime for Impl {
    fn execute(&self, _context: RequestContext, req: Frame, out: WsCallSink<String, Frame, Frame>) -> impl std::future::Future<Output = Result<(), RuntimeExecuteServerError>> + Send {
        async move {
            match req.payload {
                Some(FramePayload::Run(run)) if run.code == "watch" => {
                    tokio::spawn(async move {
                        out.closed().await;
                        if out.is_closed() { WATCHED_CLOSE.fetch_add(1, std::sync::atomic::Ordering::SeqCst); }
                    });
                }
                Some(FramePayload::Run(run)) if run.code == "boom" => {
                    return Err(RuntimeExecuteServerError::Internal("boom".into()));
                }
                Some(FramePayload::Run(run)) if run.code == "collide" => {
                    tokio::spawn(async move {
                        match out.call("dup-1".to_string(), host_call("dup-1")).await {
                            Ok(Frame { payload: Some(FramePayload::HostResult(_)) }) => {
                                let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 12 }))).await;
                            }
                            other => fail(format!("colliding call: want HostResult, got {other:?}")),
                        }
                    });
                }
                // The client's own call under the in-flight id reached the handler.
                Some(FramePayload::HostCall(call)) if call.id == "dup-1" && call.method == "client" => {
                    let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 11 }))).await;
                }
                Some(FramePayload::Run(run)) if run.code == "timeout" => {
                    tokio::spawn(async move {
                        match out.call_timeout("slow-1".to_string(), host_call("slow-1"), Duration::from_millis(200)).await {
                            Err(generated::server::WsCallError::TimedOut) => {
                                let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 7 }))).await;
                            }
                            other => fail(format!("server call_timeout: want TimedOut, got {other:?}")),
                        }
                    });
                }
                Some(FramePayload::Run(_)) => {
                    tokio::spawn(async move {
                        match out.call("call-1".to_string(), host_call("call-1")).await {
                            Ok(Frame { payload: Some(FramePayload::HostResult(result)) }) if result.id == "call-1" && result.value == "answer" => {
                                let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 0 }))).await;
                            }
                            other => fail(format!("UNEXPECTED_REPLY {other:?}")),
                        }
                    });
                }
                // The client's abandoned call reached the handler as a cancel.
                Some(FramePayload::Cancel(cancel)) if cancel.id == "c-1" => {
                    let _ = out.send(frame(FramePayload::RunResult(RunResult { exit_code: 9 }))).await;
                }
                _ => {}
            }
            Ok(())
        }
    }
}

// next_frames collects frames until a RunResult, answering HostCall("call-1").
async fn until_run_result(socket: &WsCallSocket<String, Frame, Frame>) -> (Vec<String>, i32) {
    let mut seen = Vec::new();
    while let Some(received) = socket.receive().await {
        match received.payload {
            Some(FramePayload::HostCall(call)) => {
                seen.push(format!("host_call:{}", call.id));
                if call.id == "call-1" {
                    let reply = frame(FramePayload::HostResult(HostResult { id: call.id, value: "answer".into() }));
                    socket.send(&reply).await.expect("send host result");
                }
            }
            Some(FramePayload::Cancel(cancel)) => seen.push(format!("cancel:{}", cancel.id)),
            Some(FramePayload::RunResult(result)) => return (seen, result.exit_code),
            other => fail(format!("unexpected frame {other:?}")),
        }
    }
    fail("connection closed before RunResult".to_string())
}

async fn wait_for_watched(count: usize) {
    for _ in 0..100 {
        if WATCHED_CLOSE.load(std::sync::atomic::Ordering::SeqCst) >= count { return; }
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    fail(format!("out.closed() never fired for connection {count}"));
}

// raw_close sends text on a fresh connection and returns the close code and
// reason, or None if anything other than a close frame came back first.
async fn raw_close(addr: std::net::SocketAddr, text: &str) -> Option<(u16, String)> {
    use futures_util::{SinkExt, StreamExt};
    use tokio_tungstenite::tungstenite::Message;
    let (mut ws, _) = tokio_tungstenite::connect_async(format!("ws://{addr}/v1/execute")).await.expect("raw connect");
    ws.send(Message::text(text)).await.expect("raw send");
    match ws.next().await {
        Some(Ok(Message::Close(Some(close)))) => Some((u16::from(close.code), close.reason.to_string())),
        _ => None,
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
    let run = |code: &str| frame(FramePayload::Run(RunRequest { code: code.into() }));
    let socket = client.execute(&run("x")).await.expect("connect");

    let outcome = tokio::time::timeout(Duration::from_secs(10), async {
        // Multi-variant round trip: a server call answered by a different variant.
        socket.send(&run("x")).await.expect("send run");
        let (seen, _) = until_run_result(&socket).await;
        if seen != ["host_call:call-1"] { fail(format!("round trip frames: {seen:?}")); }

        // A server call_timeout gives up with TimedOut and sends the cancel frame.
        socket.send(&run("timeout")).await.expect("send run");
        let (seen, code) = until_run_result(&socket).await;
        if seen != ["host_call:slow-1", "cancel:slow-1"] || code != 7 { fail(format!("server timeout frames: {seen:?} {code}")); }

        // A client call_timeout does the same in the other direction.
        match socket.call_timeout("c-1".to_string(), &host_call("c-1"), Duration::from_millis(100)).await {
            Err(generated::client::WsCallError::TimedOut) => {}
            other => fail(format!("client call_timeout: want TimedOut, got {other:?}")),
        }
        let (_, code) = until_run_result(&socket).await;
        if code != 9 { fail(format!("server never saw the client's cancel: {code}")); }
    })
    .await;

    let outcome = match outcome {
        Ok(()) => tokio::time::timeout(Duration::from_secs(10), async {
            // A host_call reusing the id of the server's in-flight host_call is
            // a new call for the handler (RunResult 11), not the reply; the real
            // HostResult still resolves the server's call (RunResult 12).
            socket.send(&run("collide")).await.expect("send run");
            match socket.receive().await.and_then(|f| f.payload) {
                Some(FramePayload::HostCall(call)) if call.id == "dup-1" => {}
                other => fail(format!("want the server's host_call, got {other:?}")),
            }
            let mine = frame(FramePayload::HostCall(HostCall { id: "dup-1".into(), method: "client".into() }));
            socket.send(&mine).await.expect("send colliding call");
            let (_, code) = until_run_result(&socket).await;
            if code != 11 { fail(format!("colliding call was taken for the reply: {code}")); }
            socket.send(&frame(FramePayload::HostResult(HostResult { id: "dup-1".into(), value: "ok".into() }))).await.expect("send reply");
            let (_, code) = until_run_result(&socket).await;
            if code != 12 { fail(format!("server call never resolved: {code}")); }

            // Failures close with a code and reason, with no frame first.
            let invalid = raw_close(addr, "not json").await;
            if invalid != Some((1007, "invalid JSON frame".to_string())) { fail(format!("invalid frame: {invalid:?}")); }
            let boom = raw_close(addr, r#"{"payload":{"type":"run","run":{"code":"boom"}}}"#).await;
            if boom != Some((1011, "internal server error: boom".to_string())) { fail(format!("handler error: {boom:?}")); }
        })
        .await,
        Err(elapsed) => Err(elapsed),
    };

    let outcome = match outcome {
        Ok(()) => tokio::time::timeout(Duration::from_secs(10), async {
            let fast_listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
            let fast = fast_listener.local_addr().expect("local addr");
            let options = WsServerOptions { ping_interval: Some(Duration::from_millis(50)), ..Default::default() };
            tokio::spawn(async move {
                axum::serve(fast_listener, runtime_router_with_ws_options(Arc::new(Impl), options)).await.expect("serve");
            });

            let pinged = RuntimeClient::new(format!("http://{fast}")).execute(&run("x")).await.expect("connect");
            tokio::time::sleep(Duration::from_millis(300)).await;
            pinged.send(&run("x")).await.expect("send run after pings");
            let (seen, _) = until_run_result(&pinged).await;
            if seen != ["host_call:call-1"] { fail(format!("round trip after server pings: {seen:?}")); }

            let watched = RuntimeClient::new(format!("http://{fast}")).execute(&run("x")).await.expect("connect");
            watched.send(&run("watch")).await.expect("send watch");
            tokio::time::sleep(Duration::from_millis(50)).await;
            watched.close().await;
            wait_for_watched(1).await;

            use futures_util::SinkExt;
            let (mut silent, _) = tokio_tungstenite::connect_async(format!("ws://{fast}/v1/execute")).await.expect("raw connect");
            silent.send(tokio_tungstenite::tungstenite::Message::text(r#"{"payload":{"type":"run","run":{"code":"watch"}}}"#)).await.expect("raw send");
            wait_for_watched(2).await;
            drop(silent);
        })
        .await,
        Err(elapsed) => Err(elapsed),
    };

    match outcome {
        Ok(()) => println!("OK"),
        Err(_) => fail("TIMEOUT - a frame never arrived".to_string()),
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
