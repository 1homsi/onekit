package genrust

import "testing"

const rustValidationNameFixture = `namespace app;

model GetUser {
  @minLength(1)
  @maxLength(10)
  id: string;
}

model User {
  id: string;
}

@error
model NotFoundError {
  @statusCode _: 404;
  resource: string;
}

@error
model ValidationError {
  @statusCode _: 400;
  message: string;
}

interface Users {
  @get
  @route("/users/{id}")
  get(...GetUser): User | NotFoundError | ValidationError;
}
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
	t.Parallel()
	runRustWSCrate(t, rustValidationNameFixture, "onekit-rust-validation-name", rustValidationNameMain, true)
}
