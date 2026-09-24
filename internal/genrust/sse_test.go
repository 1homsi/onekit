package genrust

import "testing"

const rustSSEFixture = `
package app
message WatchRequest { id: string }
message Tick { text: string }
service Feed { watch(WatchRequest) -> Tick @get("/w/{id}") @stream }
`

const rustSSEClientMain = `
mod generated;

use futures_util::StreamExt;
use generated::client::FeedClient;
use generated::types::WatchRequest;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

fn fail(message: String) -> ! {
    eprintln!("{message}");
    std::process::exit(1);
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let (mut socket, _) = listener.accept().await.unwrap();
        let mut request = [0u8; 4096];
        let _ = socket.read(&mut request).await;
        socket.write_all(b"HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\nconnection: close\r\n\r\n").await.unwrap();
        let mut body = Vec::new();
        body.extend_from_slice(b"data: {\"text\":\"caf\xc3");
        socket.write_all(&body).await.unwrap();
        socket.flush().await.unwrap();
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        socket.write_all(b"\xa9\"}\r\n\r\n").await.unwrap();
        let mut burst = Vec::new();
        for i in 0..2000 {
            burst.extend_from_slice(format!("data: {{\"text\":\"{i:0>600}\"}}\n\n").as_bytes());
        }
        socket.write_all(&burst).await.unwrap();
    });
    let client = FeedClient::new(format!("http://{addr}"));
    let req = WatchRequest { id: "1".into() };
    let mut stream = Box::pin(client.watch(&req).await.unwrap_or_else(|error| fail(format!("{error}"))));
    let first = stream.next().await.unwrap_or_else(|| fail("no event".into())).unwrap_or_else(|error| fail(format!("{error}")));
    if first.text != "café" {
        fail(format!("split UTF-8 or CRLF frame mangled: {:?}", first.text));
    }
    let mut count = 0;
    while let Some(item) = stream.next().await {
        item.unwrap_or_else(|error| fail(format!("burst event {count}: {error}")));
        count += 1;
    }
    if count != 2000 {
        fail(format!("expected 2000 burst events, got {count}"));
    }
    println!("OK");
}
`

func TestGeneratedRustSSEClientParsesSplitUTF8CRLFAndBursts(t *testing.T) {
	runRustWSCrate(t, rustSSEFixture, "onekit-rust-sse-client", rustSSEClientMain, true)
}
