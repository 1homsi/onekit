package genrust

import "testing"

const rustTimestampFixture = `
package app
message Event {
  at: timestamp
  on: timestamp @encode(date)
  maybe: timestamp?
}
`

const rustTimestampMain = `
mod generated;

use generated::types::Event;

fn main() {
    let ok = |at: &str, on: &str| Event { at: at.into(), on: on.into(), maybe: None }.validate().is_ok();
    for (at, on) in [("2025-01-02T03:04:05Z", "2025-01-02"), ("2025-01-02T03:04:05.123+02:00", ""), ("", "")] {
        if !ok(at, on) { eprintln!("valid timestamp rejected: {at} {on}"); std::process::exit(1); }
    }
    for (at, on) in [("yesterday", ""), ("2025-01-02 03:04:05", ""), ("2025-01-02T03:04:05", ""), ("", "2025-1-2")] {
        if ok(at, on) { eprintln!("invalid timestamp accepted: {at} {on}"); std::process::exit(1); }
    }
    if (Event { maybe: Some("nope".into()), ..Default::default() }).validate().is_ok() {
        eprintln!("invalid optional timestamp accepted");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustValidatesTimestamps(t *testing.T) {
	runRustWSCrate(t, rustTimestampFixture, "onekit-rust-timestamps", rustTimestampMain, true)
}
