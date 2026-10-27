package onek

import "testing"

func TestGoClientReturnsUnexpectedStatusError(t *testing.T) {
	buildGoSchema(t, `namespace check;

model R {
  id: string;
}

interface Items {
  @get
  @route("/items/{id}")
  get(...R): R;
}
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
