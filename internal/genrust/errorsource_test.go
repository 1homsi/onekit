package genrust

import "testing"

const rustErrorSourceFixture = `namespace app;

model R {
  id: string;
}

interface S {
  @get
  @route("/r/{id}")
  get(...R): R;
}
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
	t.Parallel()
	runRustWSCrate(t, rustErrorSourceFixture, "onekit-rust-error-source", rustErrorSourceMain, true)
}
