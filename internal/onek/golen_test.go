package onek

import "testing"

func TestGoLenCountsCharacters(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Name {
  @minLength(1)
  @maxLength(4)
  value: string;
}
`, `package api

import "testing"

func TestLen(t *testing.T) {
	if err := (&Name{Value: "éééé"}).Validate(); err != nil {
		t.Fatalf("four characters rejected: %v", err)
	}
	if err := (&Name{Value: "ééééé"}).Validate(); err == nil {
		t.Fatal("five characters accepted")
	}
}
`)
}
