package genrust

import "testing"

const rustKeywordFixture = `
package app
enum Scope { SELF  OTHER }
message Flags {
  final: bool
  override: string
  self: string
  super: string
  crate: string
  gen: int32
  scope: Scope
}
`

const rustKeywordMain = `
mod generated;

use generated::types::{Flags, Scope};

fn main() {
    let flags = Flags { r#final: true, r#override: "o".into(), self_: "s".into(), super_: "p".into(), crate_: "c".into(), r#gen: 1, scope: Scope::Self_ };
    let json = serde_json::to_string(&flags).unwrap();
    let back: Flags = serde_json::from_str(&json).unwrap();
    if !json.contains("\"final\":true") || !json.contains("\"self\":\"s\"") || !json.contains("\"scope\":\"SELF\"") || back.self_ != "s" {
        eprintln!("keyword fields lost their wire names: {json}");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustEscapesKeywordIdentifiers(t *testing.T) {
	runRustWSCrate(t, rustKeywordFixture, "onekit-rust-keywords", rustKeywordMain, true)
}
