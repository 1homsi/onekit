package genrust

import "testing"

const rustEmptyTimestampFixture = `
package app
message Event {
  name: string
  at: timestamp
  day: timestamp @encode("date")
  seen: timestamp?
}
service Events { create(Event) -> Event @post("/events") }
`

const rustEmptyTimestampMain = `mod generated;
use generated::types::Event;

fn main() {
    let unset = Event { name: "x".into(), ..Default::default() };
    assert_eq!(serde_json::to_string(&unset).unwrap(), r#"{"name":"x"}"#);
    let back: Event = serde_json::from_str(r#"{"name":"x"}"#).unwrap();
    assert_eq!(back, unset);
    let set = Event { at: "2026-01-02T03:04:05Z".into(), day: "2026-01-02".into(), ..unset.clone() };
    assert_eq!(serde_json::to_string(&set).unwrap(), r#"{"name":"x","at":"2026-01-02T03:04:05Z","day":"2026-01-02"}"#);
    println!("OK");
}
`

func TestGeneratedRustOmitsUnsetTimestamps(t *testing.T) {
	runRustWSCrate(t, rustEmptyTimestampFixture, "onekit-rust-empty-timestamp", rustEmptyTimestampMain, true)
}
