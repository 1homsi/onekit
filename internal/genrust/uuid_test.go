package genrust

import "testing"

const rustUUIDFixture = `
package app
message Ref { id: string @uuid }
`

const rustUUIDMain = `
mod generated;

use generated::types::Ref;

fn main() {
    let check = |id: &str| Ref { id: id.into() }.validate().is_ok();
    if !check("0f8fad5b-d9cb-469f-a165-70867728950e") {
        eprintln!("hyphenated UUID rejected");
        std::process::exit(1);
    }
    for id in ["{0f8fad5b-d9cb-469f-a165-70867728950e}", "urn:uuid:0f8fad5b-d9cb-469f-a165-70867728950e", "0f8fad5bd9cb469fa16570867728950e"] {
        if check(id) {
            eprintln!("non-canonical UUID accepted: {id}");
            std::process::exit(1);
        }
    }
    println!("OK");
}
`

func TestGeneratedRustUUIDMatchesGoFormat(t *testing.T) {
	runRustWSCrate(t, rustUUIDFixture, "onekit-rust-uuid", rustUUIDMain, true)
}
