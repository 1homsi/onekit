package genrust

import "testing"

const rustRejectionFixture = `
package app
message Note { text: string  count: int32 }
message ListNotes { limit: int32 @query }
service Notes {
  create(Note) -> Note @post("/notes")
  list(ListNotes) -> Note @get("/notes")
}
`

const rustRejectionMain = `
mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::Arc;

struct Impl;

impl Notes for Impl {
    fn create(&self, _context: RequestContext, req: Note) -> impl std::future::Future<Output = Result<Note, NotesCreateServerError>> + Send {
        async move { Ok(req) }
    }
    fn list(&self, _context: RequestContext, _req: ListNotes) -> impl std::future::Future<Output = Result<Note, NotesListServerError>> + Send {
        async move { Ok(Note::default()) }
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move { axum::serve(listener, notes_router(Arc::new(Impl))).await.unwrap() });
    let http = reqwest::Client::new();
    let url = format!("http://{addr}/notes");
    let check = |name: &str, status: reqwest::StatusCode, body: String, want: u16, json: bool| {
        if status.as_u16() != want || (json && !body.contains("\"message\"")) {
            eprintln!("{name}: status {status} body {body}");
            std::process::exit(1);
        }
    };
    let resp = http.post(&url).body(r#"{"text":"hi"}"#).send().await.unwrap();
    check("no content type", resp.status(), resp.text().await.unwrap(), 200, false);
    let resp = http.post(&url).send().await.unwrap();
    check("empty body", resp.status(), resp.text().await.unwrap(), 200, false);
    let resp = http.post(&url).header("content-type", "application/json").body("{nope").send().await.unwrap();
    check("malformed", resp.status(), resp.text().await.unwrap(), 400, true);
    let resp = http.post(&url).header("content-type", "application/json").body(r#"{"count":"x"}"#).send().await.unwrap();
    check("wrong type", resp.status(), resp.text().await.unwrap(), 400, true);
    let resp = http.get(format!("{url}?limit=abc")).send().await.unwrap();
    check("bad query", resp.status(), resp.text().await.unwrap(), 400, true);
    println!("OK");
}
`

func TestGeneratedRustServerAnswersBadInputWithJSON400(t *testing.T) {
	runRustWSCrate(t, rustRejectionFixture, "onekit-rust-rejections", rustRejectionMain, true)
}
