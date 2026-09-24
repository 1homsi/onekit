package onek

import "testing"

func TestGoOptionalNumberEnumCompilesAndRoundTrips(t *testing.T) {
	buildGoSchema(t, `
package check

enum Status { UNKNOWN  ACTIVE }

message Item {
  status: Status @encode(number)
  maybe: Status? @encode(number)
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestNumberEnum(t *testing.T) {
	active := StatusActive
	data, err := json.Marshal(&Item{Status: StatusActive, Maybe: &active})
	if err != nil {
		t.Fatal(err)
	}
	var out Item
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusActive || out.Maybe == nil || *out.Maybe != StatusActive {
		t.Fatalf("round trip lost data: %s -> %+v", data, out)
	}
	var empty Item
	if err := json.Unmarshal([]byte("{}"), &empty); err != nil || empty.Maybe != nil {
		t.Fatalf("absent optional enum: %v %+v", err, empty)
	}
}
`)
}
