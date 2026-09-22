package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wsFixture = `
package wsf

message ChatMessage { room: string text: string }
message ChatEvent { seq: int64 }

service ChatService {
  base_path: "/v1"

  chat(ChatMessage) -> ChatEvent @ws("/rooms/{room}")
}
`

func TestGenerateTSWSServer(t *testing.T) {
	file, err := compileForTest(wsFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateServerWithResolver(file, nil))
	for _, want := range []string{
		`export interface WSOut<E> {`,
		"export interface SocketRouteDescriptor {",
		"chat(req: ChatMessage, out: WSOut<ChatEvent>): void | Promise<void>;",
		`export function createChatServiceSocketRoutes(handler: ChatServiceHandler): SocketRouteDescriptor[] {`,
		`upgrade") || "").toLowerCase() !== "websocket"`,
		"(globalThis as any).WebSocketPair()",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated ts server missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateTSWSClient(t *testing.T) {
	file, err := compileForTest(wsFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateClientWithResolver(file, nil))
	for _, want := range []string{
		"export class ChatSocket {",
		"async chat(req: ChatMessage): Promise<ChatSocket> {",
		`.replace(/^https:/, "wss:").replace(/^http:/, "ws:")`,
		"new WebSocket(socketURL)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated ts client missing %q:\n%s", want, text)
		}
	}
}

const wsCorrelatedFixture = `
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

func TestGenerateTSWSServerCorrelated(t *testing.T) {
	file, err := compileForTest(wsCorrelatedFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateServerWithResolver(file, nil))
	for _, want := range []string{
		"export class WSPending<K, T> {",
		"export interface WSCallOut<K, E, R> extends WSOut<E> {",
		"execute(req: Frame, out: WSCallOut<string, Frame, Frame>): void | Promise<void>;",
		"const pending = new WSPending<string, Frame>();",
		"call: (id, value) => { const reply = pending.register(id); server.send(JSON.stringify(value)); return reply; },",
		"if (replyId !== undefined && pending.resolve(replyId, frame)) return;",
		// Both oneof variants carrying @ws_id must get extraction code, not
		// just whichever one happens to be first by declaration order.
		`if (frame.payload && frame.payload.type === "host_call") return frame.payload.hostCall.id;`,
		`if (frame.payload && frame.payload.type === "host_result") return frame.payload.hostResult.id;`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated correlated ts server missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateTSWSClientCorrelated(t *testing.T) {
	file, err := compileForTest(wsCorrelatedFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateClientWithResolver(file, nil))
	for _, want := range []string{
		"export class WSPending<K, T> {",
		"export class ExecuteSocket {",
		"private pending = new WSPending<string, Frame>();",
		"call(id: string, value: Frame): Promise<Frame> {",
		"private ensureListening(): void {",
		`if (frame.payload && frame.payload.type === "host_call") return frame.payload.hostCall.id;`,
		`if (frame.payload && frame.payload.type === "host_result") return frame.payload.hostResult.id;`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated correlated ts client missing %q:\n%s", want, text)
		}
	}
}

// TestGeneratedTSWSCorrelatedTypeChecks pins that a @ws_id-using server AND
// client type-check together against a real tsc - this generics/listener
// code is exactly the shape substring assertions above are weakest at
// catching.
func TestGeneratedTSWSCorrelatedTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file, err := compileForTest(wsCorrelatedFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	typesSrc := GenerateTypes(file)
	clientSrc := GenerateClientWithResolver(file, nil)
	serverSrc := GenerateServerWithResolver(file, nil)

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
