package genrust

import "testing"

const rustMaxBodySchema = `
package app

message Blob { data: string }
message Done { ok: bool }

service Uploads {
  small(Blob) -> Done @post("/small")
  big(Blob) -> Done @post("/big") @max_body("10MiB")
}
`

const rustMaxBodyMain = `mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Uploads for Impl {
    async fn small(&self, _context: RequestContext, _req: Blob) -> Result<Done, UploadsSmallServerError> { Ok(Done { ok: true }) }
    async fn big(&self, _context: RequestContext, _req: Blob) -> Result<Done, UploadsBigServerError> { Ok(Done { ok: true }) }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    let app = uploads_router(Arc::new(Impl));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let client = reqwest::Client::new();
    let post = |path: &'static str, size: usize| {
        let client = client.clone();
        let url = format!("{base}{path}");
        async move {
            match client.post(url).json(&serde_json::json!({"data": "a".repeat(size)})).send().await {
                Ok(response) => response.status().as_u16(),
                Err(_) => 413,
            }
        }
    };
    assert_eq!(post("/small", 100).await, 200);
    assert_eq!(post("/small", 9 << 20).await, 413);
    assert_eq!(post("/big", 9 << 20).await, 200);
    assert_eq!(post("/big", 11 << 20).await, 413);
    println!("OK");
}
`

func TestRustMaxBodyDecoratorSetsAPerMethodLimit(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustMaxBodySchema, "onekit-rust-max-body", rustMaxBodyMain, true)
}
