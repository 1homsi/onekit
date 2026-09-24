package genrust

import "testing"

const rustMapValidateFixture = `
package app
message Item { name: string @len(1, 5) }
message Catalog { items: map[string, Item] }
`

const rustMapValidateMain = `
mod generated;

use generated::types::{Catalog, Item};

fn main() {
    let mut catalog = Catalog::default();
    catalog.items.insert("a".into(), Item { name: "ok".into() });
    if catalog.validate().is_err() {
        eprintln!("valid map value rejected");
        std::process::exit(1);
    }
    catalog.items.insert("b".into(), Item { name: "too long".into() });
    if catalog.validate().is_ok() {
        eprintln!("invalid map value passed");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustValidatesMapMessageValues(t *testing.T) {
	runRustWSCrate(t, rustMapValidateFixture, "onekit-rust-map-validate", rustMapValidateMain, true)
}
