package genrust

import "testing"

const rustValidationNameFixture = `
package app
message GetUser { id: string @len(1, 10) }
message User { id: string }
message NotFoundError @status(404) { resource: string }
message ValidationError @status(400) { message: string }
service Users { get(GetUser) -> User | NotFoundError | ValidationError @get("/users/{id}") }
`

const rustValidationNameMain = `mod generated;
use generated::types::{GetUser, OnekitValidationError, ValidationError};

fn main() {
    let failure: OnekitValidationError = GetUser { id: String::new() }.validate().unwrap_err();
    assert_eq!(failure.field, "id");
    let declared = ValidationError { message: "bad".into() };
    assert_eq!(declared.message, "bad");
    println!("OK");
}
`

func TestGeneratedRustKeepsUserValidationErrorMessage(t *testing.T) {
	runRustWSCrate(t, rustValidationNameFixture, "onekit-rust-validation-name", rustValidationNameMain, true)
}
