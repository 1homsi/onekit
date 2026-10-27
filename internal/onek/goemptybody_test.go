package onek

import "testing"

func TestGoClientAcceptsEmptySuccessBody(t *testing.T) {
	buildGoSchema(t, `namespace check;

model DeleteRequest {
  id: string;
}

model DeleteResponse {}

interface Items {
  @delete
  @route("/items/{id}")
  remove(...DeleteRequest): DeleteResponse;
}
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	out, err := NewItemsClient(srv.URL).Remove(context.Background(), &DeleteRequest{Id: "1"})
	if err != nil || out == nil {
		t.Fatalf("204 response rejected: %v", err)
	}
}
`)
}
