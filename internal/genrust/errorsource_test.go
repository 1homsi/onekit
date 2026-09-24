package genrust

import "testing"

const rustErrorSourceFixture = `
package app
message R { id: string }
service S { get(R) -> R @get("/r/{id}") }
`

const rustErrorSourceMain = `
mod generated;

use generated::client::SClient;
use generated::types::R;

#[tokio::main]
async fn main() {
    let client = SClient::new("http://127.0.0.1:1");
    let error = client.get(&R { id: "1".into() }).await.expect_err("connection must fail");
    if std::error::Error::source(&error).is_none() {
        eprintln!("transport error has no source: {error}");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustClientErrorsExposeSource(t *testing.T) {
	runRustWSCrate(t, rustErrorSourceFixture, "onekit-rust-error-source", rustErrorSourceMain, true)
}
