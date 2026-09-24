package onek

import "testing"

func TestGoServerChecksAuthorizationScheme(t *testing.T) {
	buildGoSchema(t, `
package check

message Ping { id: string }

service Pings {
  headers: { "Authorization": string @required @auth("bearer") }
  get(Ping) -> Ping @get("/pings/{id}")
}
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pings struct{}

func (pings) Get(ctx context.Context, req *Ping) (*Ping, error) { return req, nil }

func TestScheme(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterPingsServer(mux, pings{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for value, want := range map[string]int{"garbage": 401, "Basic abc": 401, "Bearer ": 401, "Bearer abc": 200, "bearer abc": 200} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pings/1", nil)
		req.Header.Set("Authorization", value)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%q: status %d, want %d", value, resp.StatusCode, want)
		}
	}
}
`)
}
