package genrust

import "testing"

const rustWSHeadersFixture = `
package app
message M { text: string }
service S { chat(M) -> M @ws("/chat") }
`

const rustWSHeadersMain = `
mod generated;

use generated::client::SClient;
use generated::types::M;
use tokio_tungstenite::tungstenite::handshake::server::{Request, Response};

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let (tx, rx) = tokio::sync::oneshot::channel::<Option<String>>();
    tokio::spawn(async move {
        let (stream, _) = listener.accept().await.unwrap();
        let mut tx = Some(tx);
        let _ws = tokio_tungstenite::accept_hdr_async(stream, |req: &Request, resp: Response| {
            let value = req.headers().get("x-api-key").and_then(|v| v.to_str().ok()).map(str::to_owned);
            if let Some(tx) = tx.take() { let _ = tx.send(value); }
            Ok(resp)
        }).await;
        tokio::time::sleep(std::time::Duration::from_millis(200)).await;
    });
    let client = SClient::new(format!("http://{addr}")).with_header(
        reqwest::header::HeaderName::from_static("x-api-key"),
        reqwest::header::HeaderValue::from_static("secret"),
    );
    let _socket = client.chat(&M::default()).await.unwrap_or_else(|error| { eprintln!("connect: {error}"); std::process::exit(1); });
    match rx.await {
        Ok(Some(value)) if value == "secret" => println!("OK"),
        other => { eprintln!("header not sent on upgrade: {other:?}"); std::process::exit(1); }
    }
}
`

func TestGeneratedRustWSClientSendsConfiguredHeaders(t *testing.T) {
	runRustWSCrate(t, rustWSHeadersFixture, "onekit-rust-ws-headers", rustWSHeadersMain, true)
}
