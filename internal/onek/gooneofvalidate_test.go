package onek

import "testing"

func TestGoValidatesOneofMessageVariants(t *testing.T) {
	buildGoSchema(t, `namespace check;

model EmailAuth {
  @format("email")
  email: string;
  @minLength(8)
  @maxLength(72)
  password: string;
}

model TokenAuth {
  token: string;
}

union LoginRequestAuth {
  email: EmailAuth,
  token: TokenAuth,
  code: string,
}

model LoginRequest {
  auth: LoginRequestAuth;
}
`, `package api

import "testing"

func TestOneofValidation(t *testing.T) {
	bad := &LoginRequest{Auth: &LoginRequestAuthEmail{Email: &EmailAuth{Email: "a@b.co", Password: "x"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid email variant passed validation")
	}
	good := &LoginRequest{Auth: &LoginRequestAuthEmail{Email: &EmailAuth{Email: "a@b.co", Password: "long-enough"}}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (&LoginRequest{Auth: &LoginRequestAuthCode{Code: "x"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
`)
}
