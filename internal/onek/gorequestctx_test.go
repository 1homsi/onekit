package onek

import "testing"

func TestGoHandlersCanReadTheHTTPRequest(t *testing.T) {
	buildGoSchema(t, `
package check

message Ping { id: string }

service Pings {
  headers: { "X-Request-Id": string @required }
  get(Ping) -> Ping @get("/pings/{id}")
}
`, `package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pings struct{}

func (pings) Get(ctx context.Context, req *Ping) (*Ping, error) {
	r, ok := HTTPRequestFromContext(ctx)
	if !ok {
		return nil, errors.New("no request in context")
	}
	return &Ping{Id: req.Id + ":" + r.Header.Get("X-Request-Id") + ":" + r.RemoteAddr[:9]}, nil
}

func TestRequestContext(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterPingsServer(mux, pings{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewPingsClient(srv.URL)
	client.Headers["X-Request-Id"] = "abc"
	resp, err := client.Get(context.Background(), &Ping{Id: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Id != "1:abc:127.0.0.1" {
		t.Fatalf("unexpected %q", resp.Id)
	}
}
`)
}
