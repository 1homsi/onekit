package onek

import "testing"

func TestGoEnumsRejectUnknownValues(t *testing.T) {
	buildGoSchema(t, `
package check

enum Status { ACTIVE  DISABLED }

message Account {
  status: Status
  previous: Status?
  history: Status[]
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestEnumRange(t *testing.T) {
	if _, err := json.Marshal(Status(99)); err == nil {
		t.Fatal("out-of-range enum marshaled")
	}
	bad := Status(7)
	for _, account := range []*Account{{Status: 42}, {Previous: &bad}, {History: []Status{StatusActive, 9}}} {
		if account.Validate() == nil {
			t.Fatalf("invalid enum passed validation: %+v", account)
		}
	}
	if err := (&Account{Status: StatusDisabled, History: []Status{StatusActive}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
`)
}
