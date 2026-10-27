package genrust

import "testing"

const rustEmptyTimestampFixture = `namespace app;

model Event {
  name: string;
  at: utcDateTime;
  day: plainDate;
  seen?: utcDateTime;
}

interface Events {
  @post
  @route("/events")
  create(...Event): Event;
}
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
	t.Parallel()
	runRustWSCrate(t, rustEmptyTimestampFixture, "onekit-rust-empty-timestamp", rustEmptyTimestampMain, true)
}
