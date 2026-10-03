package genrust

import "testing"

const rustAuthorizeSchema = `
package app

message Principal @principal {
  user_id: string
  roles: string[]
  org: string
}

message DeleteDoc {
  id: string
  owner_org: string
}

message Ack { ok: bool }

service Docs {
  base_path: "/v1"
  delete(DeleteDoc) -> Ack @post("/docs/delete")
    @authorize("'admin' in auth.roles || auth.org == req.owner_org", "not allowed to delete this document")
    @authorize("size(auth.user_id) > 0", "sign in first")
  open(DeleteDoc) -> Ack @post("/docs/open")
}
`

const rustAuthorizeMain = `mod generated;

use axum::extract::Request;
use axum::middleware::{self, Next};
use axum::response::Response;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Docs for Impl {
    async fn delete(&self, context: RequestContext, _req: DeleteDoc) -> Result<Ack, DocsDeleteServerError> {
        assert!(context.principal.as_ref().is_some_and(|principal| !principal.user_id.is_empty()), "the handler should see the principal");
        Ok(Ack { ok: true })
    }

    async fn open(&self, context: RequestContext, _req: DeleteDoc) -> Result<Ack, DocsOpenServerError> {
        assert!(context.principal.is_none());
        Ok(Ack { ok: true })
    }
}

async fn authenticate(mut request: Request, next: Next) -> Response {
    let who = request.headers().get("x-user").and_then(|value| value.to_str().ok()).unwrap_or("").to_string();
    let principal = match who.as_str() {
        "admin" => Some(Principal { user_id: "u1".into(), roles: vec!["admin".into()], org: "acme".into() }),
        "member" => Some(Principal { user_id: "u2".into(), roles: vec![], org: "acme".into() }),
        "anonymous" => Some(Principal::default()),
        _ => None,
    };
    if let Some(principal) = principal {
        request.extensions_mut().insert(principal);
    }
    next.run(request).await
}

async fn post(base: &str, path: &str, who: &str, body: serde_json::Value) -> (u16, serde_json::Value) {
    let response = reqwest::Client::new().post(format!("{base}{path}")).header("x-user", who).json(&body).send().await.expect("send");
    let status = response.status().as_u16();
    (status, response.json().await.unwrap_or(serde_json::Value::Null))
}

#[tokio::main]
async fn main() {
    let app = docs_router(Arc::new(Impl)).layer(middleware::from_fn(authenticate));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });

    assert_eq!(post(&base, "/v1/docs/delete", "admin", serde_json::json!({"id": "1", "owner_org": "other"})).await.0, 200);
    assert_eq!(post(&base, "/v1/docs/delete", "member", serde_json::json!({"id": "1", "owner_org": "acme"})).await.0, 200);

    let (status, body) = post(&base, "/v1/docs/delete", "member", serde_json::json!({"id": "1", "owner_org": "other"})).await;
    assert_eq!(status, 403);
    assert_eq!(body["message"], "not allowed to delete this document");

    let (status, body) = post(&base, "/v1/docs/delete", "anonymous", serde_json::json!({"id": "1", "owner_org": "acme"})).await;
    assert_eq!(status, 403);
    assert_eq!(body["violations"].as_array().map(|items| items.len()), Some(2));

    assert_eq!(post(&base, "/v1/docs/delete", "stranger", serde_json::json!({"id": "1"})).await.0, 401);
    assert_eq!(post(&base, "/v1/docs/open", "stranger", serde_json::json!({"id": "1"})).await.0, 200);
    println!("OK");
}
`

func TestRustServerEnforcesAuthorize(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustAuthorizeSchema, "onekit-rust-authorize", rustAuthorizeMain, true)
}
