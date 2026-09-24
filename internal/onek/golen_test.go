package onek

import "testing"

func TestGoLenCountsCharacters(t *testing.T) {
	buildGoSchema(t, `
package check

message Name { value: string @len(1, 4) }
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
