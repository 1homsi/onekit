package genrust

import "testing"

const rustNumericInFixture = `namespace app;

model Page {
  @Onkt.values(10, 25, 50)
  size: int32;
  @Onkt.values(1, 2)
  @encode(string)
  version?: int64;
}

interface S {
  @post
  @route("/pages")
  list(...Page): Page;
}
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
	t.Parallel()
	runRustWSCrate(t, rustNumericInFixture, "onekit-rust-numeric-in", rustNumericInMain, true)
}
