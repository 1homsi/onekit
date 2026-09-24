package genrust

import "testing"

const rustFormatFixture = `
package app
message Contact {
  email: string @email
  id: string @uuid
  site: string @uri
  code: string @pattern("^[A-Z]+$")
}
`

const rustFormatMain = `
mod generated;

use generated::types::Contact;

fn main() {
    if let Err(error) = Contact::default().validate() {
        eprintln!("default message failed validation: {error}");
        std::process::exit(1);
    }
    let bad = Contact { email: "nope".into(), ..Default::default() };
    if bad.validate().is_ok() {
        eprintln!("invalid email passed");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustFormatValidatorsSkipEmptyStrings(t *testing.T) {
	runRustWSCrate(t, rustFormatFixture, "onekit-rust-format", rustFormatMain, true)
}
