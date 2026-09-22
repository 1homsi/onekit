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
		`if let Some(FramePayload::HostCall(value)) = &self.payload {`,
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
		"if let Some(id) = frame.ws_id() {",
		"if out.pending.resolve(&id, frame.clone()).await { continue; }",
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
	runRustWSCompileCheck(t, rustWSFixture, "onekit-rust-ws-fixture-plain")
}

// TestGeneratedRustWSCorrelatedCompiles is the same check for the @ws_id
// correlation path (WsPending/WsCallSink/WsCallSocket, the restructured
// multi-frame read loop, and the WsCorrelated trait impls in types.rs).
func TestGeneratedRustWSCorrelatedCompiles(t *testing.T) {
	runRustWSCompileCheck(t, rustWSCorrelatedFixture, "onekit-rust-ws-fixture-correlated")
}

func runRustWSCompileCheck(t *testing.T, src, crateName string) {
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
		"src/main.rs":             "mod generated;\nfn main() {}\n",
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

	cmd := exec.Command("cargo", "build", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust WS code failed to build: %v\n%s\n--- types.rs ---\n%s\n--- server.rs ---\n%s\n--- client.rs ---\n%s",
			err, out, types, server, client)
	}
}
