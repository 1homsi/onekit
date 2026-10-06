package genrust

import "testing"

const rustStreamErrorSchema = `
package app

message Req { id: string }
message Tick { n: int32 }
message Quota @status(429) { message: string }

service Feed {
  watch(Req) -> Tick | Quota @get("/watch") @stream
}
`

const rustStreamErrorMain = `mod generated;

use axum::response::IntoResponse;
use generated::server::*;
use generated::types::*;
use std::pin::Pin;
use std::sync::Arc;

struct Impl;

impl Feed for Impl {
    async fn watch(
        &self,
        _context: RequestContext,
        req: Req,
    ) -> Result<Pin<Box<dyn futures_util::Stream<Item = Result<Tick, FeedWatchServerError>> + Send>>, FeedWatchServerError> {
        let items: Vec<Result<Tick, FeedWatchServerError>> = match req.id.as_str() {
            "quota" => vec![Ok(Tick { n: 1 }), Err(FeedWatchServerError::Quota(Quota { message: "daily quota reached".into() }))],
            _ => vec![Ok(Tick { n: 1 }), Err(FeedWatchServerError::Internal("db password is hunter2".into()))],
        };
        Ok(Box::pin(futures_util::stream::iter(items)))
    }
}

async fn read(base: &str, id: &str) -> String {
    reqwest::get(format!("{base}/watch?id={id}")).await.unwrap().text().await.unwrap()
}

async fn serve(router: axum::Router) -> String {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    tokio::spawn(async move { axum::serve(listener, router).await.expect("serve") });
    base
}

#[tokio::main]
async fn main() {
    let plain = serve(feed_router(Arc::new(Impl))).await;
    let got = read(&plain, "quota").await;
    assert!(got.contains("event: error\ndata: {\"message\":\"daily quota reached\"}"), "declared error keeps its body: {got}");
    let got = read(&plain, "boom").await;
    assert!(got.contains("event: error\ndata: {\"message\":\"internal server error\"}"), "{got}");

    let writer: ErrorWriter = Arc::new(|info, _headers| {
        let body = serde_json::json!({"error": {"code": info.code, "message": info.message}});
        (info.status, axum::Json(body)).into_response()
    });
    let wrapped = serve(with_error_writer(feed_router(Arc::new(Impl)), writer)).await;
    let got = read(&wrapped, "boom").await;
    assert!(got.contains("event: error\ndata: {\"error\":{\"code\":\"internal\",\"message\":\"internal server error\"}}"), "configured envelope mid-stream: {got}");
    assert!(!got.contains("hunter2"), "{got}");
    let got = read(&wrapped, "quota").await;
    assert!(got.contains("{\"message\":\"daily quota reached\"}"), "declared error body is never re-rendered: {got}");
    println!("OK");
}
`

func TestRustMidStreamErrorsUseTheConfiguredErrorWriter(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustStreamErrorSchema, "onekit-rust-stream-error", rustStreamErrorMain, true)
}
