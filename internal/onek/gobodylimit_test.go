package onek

import "testing"

func TestGoServerBodyLimitOptionAnd413(t *testing.T) {
	buildGoSchema(t, `
package check

message Note { text: string }

service Notes { create(Note) -> Note @post("/notes") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type notes struct{}

func (notes) Create(ctx context.Context, req *Note) (*Note, error) { return req, nil }

func TestBodyLimit(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterNotesServer(mux, notes{}, WithMaxRequestBodyBytes(64)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	big := `+"`"+`{"text":"`+"`"+` + strings.Repeat("x", 100) + `+"`"+`"}`+"`"+`
	resp, err := http.Post(srv.URL+"/notes", "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", resp.StatusCode)
	}
	resp, err = http.Post(srv.URL+"/notes", "application/json", strings.NewReader(`+"`"+`{"text":"ok"}`+"`"+`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("small body rejected: %v %v", err, resp.StatusCode)
	}
}
`)
}
