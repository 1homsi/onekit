package onek

import "testing"

func TestGoRequiredOneof(t *testing.T) {
	buildGoSchema(t, `namespace check;

model A {
  v: string;
}

union MP {
  a: A,
  s: string,
}

model M {
  @Onkt.required
  p: MP;
}
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
