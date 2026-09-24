package onek

import "testing"

func TestGoValidatesOneofMessageVariants(t *testing.T) {
	buildGoSchema(t, `
package check

message EmailAuth { email: string @email  password: string @len(8, 72) }
message TokenAuth { token: string }

message LoginRequest {
  auth: oneof { email: EmailAuth  token: TokenAuth  code: string }
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
