package genrust

import "testing"

const rustNumericInFixture = `
package app
message Page {
  size: int32 @in(10, 25, 50)
  version: int64? @in(1, 2)
}
service S { list(Page) -> Page @post("/pages") }
`

const rustNumericInMain = `mod generated;
use generated::types::Page;

fn main() {
    let ok = Page { size: 25, version: Some(2) };
    assert!(ok.validate().is_ok());
    for bad in [
        Page { size: 30, ..ok.clone() },
        Page { version: Some(3), ..ok.clone() },
    ] {
        assert!(bad.validate().is_err());
    }
    println!("OK");
}
`

func TestGeneratedRustIntegerInValidation(t *testing.T) {
	runRustWSCrate(t, rustNumericInFixture, "onekit-rust-numeric-in", rustNumericInMain, true)
}
