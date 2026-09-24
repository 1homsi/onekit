package genrust

import "testing"

const rustBodyLimitFixture = `
package app
message Note { text: string }
service Notes { create(Note) -> Note @post("/notes") }
`

const rustBodyLimitMain = `
mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Notes for Impl {
    fn create(&self, _context: RequestContext, req: Note) -> impl std::future::Future<Output = Result<Note, NotesCreateServerError>> + Send {
        async move { Ok(req) }
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move { axum::serve(listener, notes_router(Arc::new(Impl))).await.unwrap() });
    let body = format!("{{\"text\":\"{}\"}}", "x".repeat(4 << 20));
    let response = reqwest::Client::new().post(format!("http://{addr}/notes")).header("content-type", "application/json").body(body).send().await.unwrap();
    if response.status() != 200 {
        eprintln!("4 MiB body rejected with {}", response.status());
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustServerAccepts8MiBBodies(t *testing.T) {
	runRustWSCrate(t, rustBodyLimitFixture, "onekit-rust-body-limit", rustBodyLimitMain, true)
}
