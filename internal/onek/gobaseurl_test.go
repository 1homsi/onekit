package onek

import "testing"

func TestGoClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	buildGoSchema(t, `
package check

message Item { id: string  name: string }

service Items { create(Item) -> Item @post("/items") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type items struct{}

func (items) Create(ctx context.Context, req *Item) (*Item, error) { return req, nil }

func TestTrailingSlash(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterItemsServer(mux, items{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out, err := NewItemsClient(srv.URL+"/").Create(context.Background(), &Item{Id: "1", Name: "a"})
	if err != nil || out.Name != "a" {
		t.Fatalf("post through trailing-slash base URL failed: %v %+v", err, out)
	}
}
`)
}
