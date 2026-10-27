package genrust

import "testing"

const rustKeywordFixture = "namespace app;\n\nenum Scope {\n  SELF,\n  OTHER,\n}\n\nmodel Flags {\n  final: boolean;\n  override: string;\n  `self`: string;\n  `super`: string;\n  crate: string;\n  gen: int32;\n  scope: Scope;\n}\n"

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
	t.Parallel()
	runRustWSCrate(t, rustKeywordFixture, "onekit-rust-keywords", rustKeywordMain, true)
}
