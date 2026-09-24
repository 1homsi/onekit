package onek

import "testing"

func TestGoClientReturnsUnexpectedStatusError(t *testing.T) {
	buildGoSchema(t, `
package check

message R { id: string }

service Items { get(R) -> R @get("/items/{id}") }
`, `package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := NewItemsClient(srv.URL).Get(context.Background(), &R{Id: "1"})
	var statusErr *UnexpectedStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != 503 || statusErr.Header.Get("Retry-After") != "3" || string(statusErr.Body) != "busy\n" {
		t.Fatalf("want UnexpectedStatusError 503, got %#v", err)
	}
}
`)
}
