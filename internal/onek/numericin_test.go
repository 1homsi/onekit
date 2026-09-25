package onek

import "testing"

func TestGoIntegerInValidation(t *testing.T) {
	buildGoSchema(t, `
package check

message Page {
  size: int32 @in(10, 25, 50)
  version: uint64? @in(1, 2)
}
`, `package api

import "testing"

func TestIntegerIn(t *testing.T) {
	two := uint64(2)
	if err := (&Page{Size: 25, Version: &two}).Validate(); err != nil {
		t.Fatalf("valid page rejected: %v", err)
	}
	if err := (&Page{Size: 30}).Validate(); err == nil {
		t.Fatal("size 30 accepted")
	}
	three := uint64(3)
	if err := (&Page{Size: 10, Version: &three}).Validate(); err == nil {
		t.Fatal("version 3 accepted")
	}
}
`)
}
