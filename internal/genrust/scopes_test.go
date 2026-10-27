package genrust

import (
	"strings"
	"testing"
)

const rustScopesSchema = `namespace app;

model Item {
  id: string;
}

model GetItem {
  id: string;
}

@error
model Forbidden {
  @statusCode _: 403;
  message: string;
}

@route("/items/v1")
interface Items {
  @get
  @route("/items/{id}")
  @Onkt.requires("items:read")
  get(...GetItem): Item | Forbidden;

  @delete
  @route("/items/{id}")
  @Onkt.requires("items:read", "items:write")
  remove(...GetItem): Item | Forbidden;

  @get
  @route("/ping/{id}")
  ping(...GetItem): Item | Forbidden;
}
`

func TestRustContextCarriesRequiredScopes(t *testing.T) {
	out := string(GenerateServer(compileRustSchema(t, rustScopesSchema)))
	for _, want := range []string{
		"pub required_scopes: &'static [&'static str],",
		"pub fn missing_scopes<S: AsRef<str>>(&self, granted: &[S]) -> Vec<&'static str> {",
		`required_scopes: &["items:read"], meta: &[] };`,
		`required_scopes: &["items:read", "items:write"], meta: &[] };`,
		"required_scopes: &[], meta: &[] };",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

const rustScopesMain = `mod generated;

use generated::client::*;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

fn forbid_unless(context: &RequestContext, granted: &[&str]) -> Result<(), Forbidden> {
    let missing = context.missing_scopes(granted);
    if missing.is_empty() {
        Ok(())
    } else {
        Err(Forbidden { message: format!("missing required scope: {}", missing.join(", ")) })
    }
}

impl Items for Impl {
    async fn get(&self, context: RequestContext, req: GetItem) -> Result<Item, ItemsGetServerError> {
        forbid_unless(&context, &["items:read"]).map_err(ItemsGetServerError::Forbidden)?;
        Ok(Item { id: req.id })
    }

    async fn remove(&self, context: RequestContext, req: GetItem) -> Result<Item, ItemsRemoveServerError> {
        forbid_unless(&context, &["items:read"]).map_err(ItemsRemoveServerError::Forbidden)?;
        Ok(Item { id: req.id })
    }

    async fn ping(&self, context: RequestContext, _req: GetItem) -> Result<Item, ItemsPingServerError> {
        Ok(Item { id: format!("{}", context.required_scopes.len()) })
    }
}

#[tokio::main]
async fn main() {
    let app = items_router(Arc::new(Impl));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let client = ItemsClient::new(base);

    let ok = client.get(&GetItem { id: "7".into() }).await.expect("get with the granted scope");
    assert_eq!(ok.id, "7");

    match client.remove(&GetItem { id: "7".into() }).await {
        Err(ItemsRemoveError::Forbidden(error)) => assert_eq!(error.message, "missing required scope: items:write"),
        other => panic!("expected a 403 naming the missing scope, got {other:?}"),
    }

    let ping = client.ping(&GetItem { id: "1".into() }).await.expect("ping");
    assert_eq!(ping.id, "0");
    println!("OK");
}
`

func TestRustServerHandlersSeeRequiredScopes(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustScopesSchema, "onekit-rust-scopes", rustScopesMain, true)
}
