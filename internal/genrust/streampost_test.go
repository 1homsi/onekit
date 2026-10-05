package genrust

import "testing"

const rustStreamPostSchema = `
package app

message TurnRequest { prompt: string }
message Text { text: string }
message Done { reason: string }
message TurnEvent {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
    done: Done @tag("done")
  }
}

service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream
}
`

const rustStreamPostMain = `mod generated;

use futures_util::StreamExt;
use generated::client::*;
use generated::server::*;
use generated::types::*;
use std::pin::Pin;
use std::sync::Arc;

struct Impl;

impl Agent for Impl {
    async fn turn(
        &self,
        _context: RequestContext,
        req: TurnRequest,
    ) -> Result<Pin<Box<dyn futures_util::Stream<Item = Result<TurnEvent, AgentTurnServerError>> + Send>>, AgentTurnServerError> {
        let events = vec![
            Ok(TurnEvent { payload: Some(TurnEventPayload::Text(Text { text: format!("hi {}", req.prompt) })) }),
            Ok(TurnEvent { payload: Some(TurnEventPayload::Done(Done { reason: "stop".into() })) }),
        ];
        Ok(Box::pin(futures_util::stream::iter(events)))
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let base = format!("http://{}", listener.local_addr().expect("addr"));
    let app = agent_router(Arc::new(Impl));
    tokio::spawn(async move { axum::serve(listener, app).await.expect("serve") });
    let raw = reqwest::Client::new()
        .post(format!("{base}/turn"))
        .json(&serde_json::json!({"prompt": "bob"}))
        .send()
        .await
        .unwrap()
        .text()
        .await
        .unwrap();
    assert!(raw.contains("event: text"), "{raw}");
    assert!(raw.contains("event: done"), "{raw}");
    let client = AgentClient::new(base);
    let request = TurnRequest { prompt: "amy".into() };
    let mut stream = Box::pin(client.turn(&request).await.unwrap());
    let mut seen = Vec::new();
    while let Some(item) = stream.next().await {
        seen.push(format!("{:?}", item.unwrap().payload));
    }
    assert_eq!(seen.len(), 2, "{seen:?}");
    assert!(seen[0].contains("hi amy"), "{seen:?}");
    println!("OK");
}
`

func TestRustPostStreamWithTypedEvents(t *testing.T) {
	t.Parallel()
	runRustWSCrate(t, rustStreamPostSchema, "onekit-rust-stream-post", rustStreamPostMain, true)
}
