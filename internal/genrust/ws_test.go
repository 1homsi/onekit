package genrust

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
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
	} {
		if !strings.Contains(string(server), want) {
			t.Fatalf("generated rust server missing %q:\n%s", want, server)
		}
	}

	client := GenerateClient(file)
	for _, want := range []string{
		"pub struct WsFrameSocket<E> {",
		"tokio_tungstenite::connect_async(url)",
		"pub async fn chat(&self, req: &ChatMessage) -> Result<ChatServiceChatSocket<ChatEvent>, ChatServiceChatError>",
	} {
		if !strings.Contains(string(client), want) {
			t.Fatalf("generated rust client missing %q:\n%s", want, client)
		}
	}
}
