package genrust

import "testing"

const rustErrorSchema = `
package app

message Draft {
  id: int64
  name: string @len(2, 10)
}
message Done { ok: bool }

service Things {
  base_path: "/v1"
  save(Draft) -> Done @post("/things/save")
}
`

const rustErrorMain = `mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Things for Impl {
    async fn save(&self, _context: RequestContext, _req: Draft) -> Result<Done, ThingsSaveServerError> {
        Ok(Done { ok: true })
    }
}

async fn serve(app: axum::Router) -> String {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    base
}

async fn send(method: reqwest::Method, url: String, body: &str) -> (u16, Option<String>, serde_json::Value) {
    let response = reqwest::Client::new().request(method, url).header("content-type", "application/json").body(body.to_string()).send().await.expect("send");
    let status = response.status().as_u16();
    let allow = response.headers().get("allow").and_then(|value| value.to_str().ok()).map(str::to_string);
    (status, allow, response.json().await.unwrap_or(serde_json::Value::Null))
}

#[tokio::main]
async fn main() {
    let writer: ErrorWriter = Arc::new(|info: &ServerErrorInfo, _headers: &axum::http::HeaderMap| {
        let body = serde_json::json!({ "error": { "code": info.code, "message": info.message, "violations": info.violations } });
        (info.status, axum::Json(body)).into_response()
    });
    use axum::response::IntoResponse as _;
    let custom = serve(with_error_writer(things_router(Arc::new(Impl)), writer)).await;
    let plain = serve(things_router(Arc::new(Impl))).await;

    let (status, _, body) = send(reqwest::Method::POST, format!("{custom}/v1/things/save"), "{not json").await;
    assert_eq!(status, 400);
    assert_eq!(body["error"]["code"], "invalid_request");

    let (status, _, body) = send(reqwest::Method::POST, format!("{custom}/v1/things/save"), r#"{"id":"1","name":"x"}"#).await;
    assert_eq!(status, 400);
    assert_eq!(body["error"]["code"], "validation_failed");

    let (status, _, body) = send(reqwest::Method::GET, format!("{custom}/nope"), "").await;
    assert_eq!(status, 404);
    assert_eq!(body["error"]["code"], "not_found");

    let (status, allow, body) = send(reqwest::Method::GET, format!("{custom}/v1/things/save"), "").await;
    assert_eq!(status, 405);
    assert_eq!(body["error"]["code"], "method_not_allowed");
    assert_eq!(allow.as_deref(), Some("POST"));

    let (status, _, body) = send(reqwest::Method::POST, format!("{custom}/v1/things/save"), r#"{"id":"1","name":"fine"}"#).await;
    assert_eq!((status, body["ok"].as_bool()), (200, Some(true)));

    let (status, _, body) = send(reqwest::Method::POST, format!("{plain}/v1/things/save"), "{not json").await;
    assert_eq!(status, 400);
    assert!(body.get("message").is_some() && body.get("error").is_none(), "{body}");
    println!("OK");
}
`

func TestRustServerErrorWriterRewritesEveryGenericError(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustErrorSchema, "onekit-rust-error-writer", rustErrorMain, true)
}
