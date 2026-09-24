package genrust

import "testing"

const rustWSQueryFixture = `
package app
message Msg {
  room: string
  limit: int32? @query
  tags: string[] @query("tag")
  text: string
}
service Chat { chat(Msg) -> Msg @ws("/rooms/{room}") }
`

const rustWSQueryMain = `
mod generated;

use generated::client::ChatClient;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Chat for Impl {
    fn chat(&self, _context: RequestContext, req: Msg, out: WsSink<Msg>) -> impl std::future::Future<Output = Result<(), ChatChatServerError>> + Send {
        async move {
            let _ = out.send(req).await;
            Ok(())
        }
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move { axum::serve(listener, chat_router(Arc::new(Impl))).await.unwrap() });
    let client = ChatClient::new(format!("http://{addr}"));
    let connect = Msg { room: "lobby".into(), limit: Some(5), tags: vec!["a".into(), "b c".into()], text: String::new() };
    let socket = client.chat(&connect).await.unwrap_or_else(|error| { eprintln!("connect: {error}"); std::process::exit(1); });
    socket.send(&Msg { text: "hi".into(), ..Default::default() }).await.unwrap();
    let echo = tokio::time::timeout(std::time::Duration::from_secs(5), socket.receive()).await.ok().flatten();
    match echo {
        Some(Ok(msg)) if msg.room == "lobby" && msg.limit == Some(5) && msg.tags == vec!["a".to_string(), "b c".to_string()] && msg.text == "hi" => println!("OK"),
        other => { eprintln!("bound values missing from frame: {other:?}"); std::process::exit(1); }
    }
}
`

func TestGeneratedRustWSBindsPathAndQueryIntoFrames(t *testing.T) {
	runRustWSCrate(t, rustWSQueryFixture, "onekit-rust-ws-query", rustWSQueryMain, true)
}
