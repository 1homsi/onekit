package onek

import "testing"

func TestGoHandlersControlStatusAndHeaders(t *testing.T) {
	buildGoSchema(t, `
package check

message Item { id: string  name: string }

service Items { create(Item) -> Item @post("/items") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type items struct{}

func (items) Create(ctx context.Context, req *Item) (*Item, error) {
	SetResponseStatus(ctx, http.StatusCreated)
	ResponseHeader(ctx).Set("Location", "/items/"+req.Id)
	return req, nil
}

func TestResponseControl(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterItemsServer(mux, items{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/items", "application/json", strings.NewReader(`+"`"+`{"id":"7","name":"x"}`+"`"+`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("Location") != "/items/7" {
		t.Fatalf("status %d location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, err := NewItemsClient(srv.URL).Create(context.Background(), &Item{Id: "8"}); err != nil {
		t.Fatalf("client rejected 201: %v", err)
	}
}
`)
}
