package onek

import "testing"

func TestGoErrorsCanExposeAPublicMessage(t *testing.T) {
	buildGoSchema(t, `
package check

message Ping { id: string }

service Pings { get(Ping) -> Ping @get("/pings/{id}") }
`, `package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type quotaError struct{}

func (quotaError) Error() string          { return "tenant 42 exceeded quota in shard db-7" }
func (quotaError) HTTPStatusCode() int    { return http.StatusTooManyRequests }
func (quotaError) PublicMessage() string  { return "rate limit exceeded" }

type pings struct{}

func (pings) Get(ctx context.Context, req *Ping) (*Ping, error) { return nil, quotaError{} }

func TestPublicMessage(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterPingsServer(mux, pings{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/pings/1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != 429 || body["message"] != "rate limit exceeded" {
		t.Fatalf("got %d %v", resp.StatusCode, body)
	}
}
`)
}
