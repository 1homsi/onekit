package onek

import "testing"

func TestGoRequiredOneof(t *testing.T) {
	buildGoSchema(t, `
package check

message A { v: string }
message M { p: oneof { a: A  s: string } @required }
`, `package api

import "testing"

func TestRequired(t *testing.T) {
	if (&M{}).Validate() == nil {
		t.Fatal("missing oneof passed")
	}
	if err := (&M{P: &MPS{S: "x"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
`)
}
