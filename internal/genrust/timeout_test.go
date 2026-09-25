package genrust

import "testing"

const rustTimeoutFixture = `
package app
message R { id: string }
service S { get(R) -> R @get("/r/{id}") }
`

const rustTimeoutMain = `
mod generated;

use generated::client::SClient;
use generated::types::R;

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let (_socket, _) = listener.accept().await.unwrap();
        tokio::time::sleep(std::time::Duration::from_secs(10)).await;
    });
    let client = SClient::new(format!("http://{addr}")).with_timeout(Some(std::time::Duration::from_millis(200)));
    let started = std::time::Instant::now();
    let result = client.get(&R { id: "1".into() }).await;
    if result.is_ok() || started.elapsed() > std::time::Duration::from_secs(3) {
        eprintln!("stalled server did not time out: {:?}", started.elapsed());
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustClientTimesOut(t *testing.T) {
	runRustWSCrate(t, rustTimeoutFixture, "onekit-rust-timeout", rustTimeoutMain, true)
}
