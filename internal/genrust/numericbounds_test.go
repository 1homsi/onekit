package genrust

import "testing"

const rustNumericBoundsFixture = `
package app
message Bounds {
  price: float64 @gte(0) @lte(100)
  ratio: float32 @range(0, 1)
  count: int32 @range(1, 1000000)
  small: uint32 @lt(10)
  weight: float64 @range(-1.5, 1e3)
  offset: int64 @gt(-5)
}
service S { check(Bounds) -> Bounds @post("/check") }
`

const rustNumericBoundsMain = `mod generated;
use generated::types::Bounds;

fn main() {
    let ok = Bounds { price: 50.5, ratio: 0.5, count: 10, small: 3, offset: 0, weight: 2.0 };
    assert!(ok.validate().is_ok());
    for bad in [
        Bounds { price: -0.5, ..ok.clone() },
        Bounds { ratio: 1.5, ..ok.clone() },
        Bounds { count: 2_000_000, ..ok.clone() },
        Bounds { small: 10, ..ok.clone() },
        Bounds { offset: -5, ..ok.clone() },
        Bounds { weight: 1001.0, ..ok.clone() },
    ] {
        assert!(bad.validate().is_err());
    }
    println!("OK");
}
`

func TestGeneratedRustNumericBoundsCompileForEveryScalar(t *testing.T) {
	runRustWSCrate(t, rustNumericBoundsFixture, "onekit-rust-numeric-bounds", rustNumericBoundsMain, true)
}
