package genrust

import "testing"

const rustNullableSchema = `
package app

message Item { name: string @len(1, 10) }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable @len(1, 5)
  item: Item? @nullable
}

service Patches {
  patch(Patch) -> Patch @patch("/patch")
}
`

const rustNullableMain = `mod generated;

use generated::client::*;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Patches for Impl {
    async fn patch(&self, _context: RequestContext, req: Patch) -> Result<Patch, PatchesPatchServerError> {
        Ok(req)
    }
}

#[tokio::main]
async fn main() {
    let absent: Patch = serde_json::from_str(r#"{"id":1}"#).unwrap();
    assert!(absent.folder_id.is_none() && absent.note.is_none() && absent.item.is_none());
    assert_eq!(serde_json::to_string(&absent).unwrap(), r#"{"id":1}"#);

    let nulled: Patch = serde_json::from_str(r#"{"id":1,"folder_id":null,"note":"x","item":null}"#).unwrap();
    assert_eq!(nulled.folder_id, Some(None));
    assert_eq!(nulled.note, Some(Some("x".to_string())));
    assert_eq!(nulled.item, Some(None));
    let value: serde_json::Value = serde_json::to_value(&nulled).unwrap();
    assert_eq!(value, serde_json::json!({"id":1,"folder_id":null,"note":"x","item":null}));

    let set = Patch { id: 1, folder_id: Some(Some(7)), note: None, item: Some(Some(Box::new(Item { name: "n".into() }))) };
    assert_eq!(serde_json::to_value(&set).unwrap(), serde_json::json!({"id":1,"folder_id":7,"item":{"name":"n"}}));
    assert!(Patch { id: 1, folder_id: None, note: Some(Some("toolong".into())), item: None }.validate().is_err());
    assert!(Patch { id: 1, folder_id: None, note: Some(None), item: Some(None) }.validate().is_ok());

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    let app = patches_router(Arc::new(Impl));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let client = PatchesClient::new(base);
    let got = client.patch(&Patch { id: 2, folder_id: Some(None), note: None, item: None }).await.unwrap();
    assert_eq!(got.folder_id, Some(None));
    assert!(got.note.is_none());
    println!("OK");
}
`

func TestRustNullableFieldsKeepAbsentNullAndValueApart(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustNullableSchema, "onekit-rust-nullable", rustNullableMain, true)
}
