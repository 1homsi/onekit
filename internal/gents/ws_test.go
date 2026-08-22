package gents

import (
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
