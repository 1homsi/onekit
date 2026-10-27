package genrust

import "testing"

const rustMetaSchema = `namespace app;

model Doc {
  slug: string;
}

model Done {
  ok: boolean;
}

@route("/v1")
interface Docs {
  @post
  @route("/apps/{slug}/edit")
  @Onkt.meta("guard", "app/edit/:slug")
  @Onkt.meta("audit.event", "app.update")
  edit(...Doc): Done;

  @get
  @route("/health/{slug}")
  health(...Doc): Done;
}
`

const rustMetaMain = `mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::{Arc, Mutex};

struct Impl {
    seen: Mutex<Vec<String>>,
}

impl Docs for Impl {
    async fn edit(&self, context: RequestContext, _req: Doc) -> Result<Done, DocsEditServerError> {
        self.seen.lock().unwrap().push(format!("{:?} {:?}", context.meta_value("guard"), context.meta_value("audit.event")));
        Ok(Done { ok: true })
    }

    async fn health(&self, context: RequestContext, _req: Doc) -> Result<Done, DocsHealthServerError> {
        self.seen.lock().unwrap().push(format!("{} {:?}", context.meta.len(), context.meta_value("guard")));
        Ok(Done { ok: true })
    }
}

#[tokio::main]
async fn main() {
    let service = Arc::new(Impl { seen: Mutex::new(Vec::new()) });
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    let app = docs_router(service.clone());
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let client = reqwest::Client::new();
    assert!(client.post(format!("{base}/v1/apps/mine/edit")).json(&serde_json::json!({})).send().await.unwrap().status().is_success());
    assert!(client.get(format!("{base}/v1/health/x")).send().await.unwrap().status().is_success());
    let seen = service.seen.lock().unwrap().clone();
    assert_eq!(seen, vec![r#"Some("app/edit/:slug") Some("app.update")"#.to_string(), "0 None".to_string()]);
    println!("OK");
}
`

func TestRustRouteMetadataReachesHandlers(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustMetaSchema, "onekit-rust-route-meta", rustMetaMain, true)
}
