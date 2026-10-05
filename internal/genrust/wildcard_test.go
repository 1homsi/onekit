package genrust

import "testing"

const rustWildcardSchema = `
package app

message FileRef { path: string }
message Content { path: string }
message Empty {}

service Files {
  base_path: "/files"
  read(FileRef) -> Content @get("/{path...}")
  root(Empty) -> Content @get("")
}
`

const rustWildcardMain = `mod generated;

use generated::client::*;
use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Files for Impl {
    async fn read(&self, _context: RequestContext, req: FileRef) -> Result<Content, FilesReadServerError> {
        Ok(Content { path: req.path })
    }

    async fn root(&self, _context: RequestContext, _req: Empty) -> Result<Content, FilesRootServerError> {
        Ok(Content { path: "ROOT".into() })
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    let app = files_router(Arc::new(Impl));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let raw = reqwest::Client::new();
    for (path, want) in [
        ("/files/a.txt", "a.txt"),
        ("/files/dir/sub/b.txt", "dir/sub/b.txt"),
        ("/files/sp%20ace/x%2By", "sp ace/x+y"),
        ("/files", "ROOT"),
    ] {
        let body: serde_json::Value = raw.get(format!("{base}{path}")).send().await.unwrap().json().await.unwrap();
        assert_eq!(body["path"], want, "{path}");
    }
    let client = FilesClient::new(base);
    for path in ["a.txt", "dir/sub/b.txt", "sp ace/x+y", "100%/ü.txt"] {
        let got = client.read(&FileRef { path: path.into() }).await.unwrap();
        assert_eq!(got.path, path);
    }
    assert!(client.read(&FileRef { path: "a/../../admin".into() }).await.is_err());
    assert_eq!(client.root(&Empty {}).await.unwrap().path, "ROOT");
    println!("OK");
}
`

func TestRustWildcardPathsAndEmptyRoutes(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustWildcardSchema, "onekit-rust-wildcard", rustWildcardMain, true)
}
