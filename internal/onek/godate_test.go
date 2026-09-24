package onek

import "testing"

func TestGoDateFieldsCompileAndTolerateAbsentValues(t *testing.T) {
	buildGoSchema(t, `
package check

message Trip {
  start: timestamp @encode(date)
  end: timestamp @encode(date)
}
`, `package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDates(t *testing.T) {
	var empty Trip
	if err := json.Unmarshal([]byte("{}"), &empty); err != nil {
		t.Fatalf("absent date rejected: %v", err)
	}
	if !empty.Start.IsZero() || !empty.End.IsZero() {
		t.Fatalf("absent dates not zero: %+v", empty)
	}
	var trip Trip
	if err := json.Unmarshal([]byte(`+"`"+`{"start":"2026-01-02","end":"2026-01-05"}`+"`"+`), &trip); err != nil {
		t.Fatal(err)
	}
	if trip.End.Sub(trip.Start) != 72*time.Hour {
		t.Fatalf("dates decoded wrong: %+v", trip)
	}
}
`)
}
