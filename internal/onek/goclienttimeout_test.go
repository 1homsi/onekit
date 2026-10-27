package onek

import "testing"

func TestGoClientBoundsTheWaitForResponseHeaders(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Ping {
  id: string;
}

interface Pings {
  @get
  @route("/pings/{id}")
  get(...Ping): Ping;
}
`, `package api

import (
	"net/http"
	"testing"
	"time"
)

func TestDefaultClientTimeout(t *testing.T) {
	client := NewPingsClient("http://example.test")
	transport, ok := client.HTTPClient.Transport.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout != 30*time.Second {
		t.Fatalf("transport = %#v", client.HTTPClient.Transport)
	}
	if client.HTTPClient.Timeout != 0 {
		t.Fatal("a whole-request timeout would cut long-lived SSE streams")
	}
	if client.HTTPClient == http.DefaultClient {
		t.Fatal("still using http.DefaultClient")
	}
}
`)
}
