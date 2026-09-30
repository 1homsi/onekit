package onek

import "testing"

func TestGoServerEnforcesRequiredScopes(t *testing.T) {
	buildGoSchema(t, `
package check

message Item { id: string }
message GetItem { id: string }
message Nothing {}

service Items {
  base_path: "/items/v1"

  get(GetItem) -> Item @get("/items/{id}") @requires("items:read")
  remove(GetItem) -> Nothing @delete("/items/{id}") @requires("items:read", "items:write")
  ping(Nothing) -> Nothing @get("/ping")
}
`, `package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type items struct{}

func (items) Get(ctx context.Context, req *GetItem) (*Item, error)       { return &Item{Id: req.Id}, nil }
func (items) Remove(ctx context.Context, req *GetItem) (*Nothing, error) { return &Nothing{}, nil }
func (items) Ping(ctx context.Context, req *Nothing) (*Nothing, error)   { return &Nothing{}, nil }

func serve(t *testing.T, granted func(*http.Request) ([]string, error)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	err := RegisterItemsServer(mux, items{}, WithScopes(func(ctx context.Context, r *http.Request) ([]string, error) { return granted(r) }))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func status(t *testing.T, method, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	var chunk [512]byte
	for {
		n, readErr := resp.Body.Read(chunk[:])
		buf.Write(chunk[:n])
		if readErr != nil {
			break
		}
	}
	return resp.StatusCode, buf.String()
}

func TestScopes(t *testing.T) {
	read := serve(t, func(r *http.Request) ([]string, error) { return []string{"items:read"}, nil })
	if code, _ := status(t, "GET", read.URL+"/items/v1/items/7"); code != http.StatusOK {
		t.Fatalf("holding the scope must pass, got %d", code)
	}
	code, body := status(t, "DELETE", read.URL+"/items/v1/items/7")
	if code != http.StatusForbidden || !strings.Contains(body, "missing required scope: items:write") {
		t.Fatalf("a missing scope must be 403 naming it, got %d %s", code, body)
	}
	if code, _ := status(t, "GET", read.URL+"/items/v1/ping"); code != http.StatusOK {
		t.Fatalf("routes without @requires stay open, got %d", code)
	}

	none := serve(t, func(r *http.Request) ([]string, error) { return nil, nil })
	code, body = status(t, "DELETE", none.URL+"/items/v1/items/7")
	if code != http.StatusForbidden || !strings.Contains(body, "items:read, items:write") {
		t.Fatalf("every missing scope is listed, got %d %s", code, body)
	}

	anon := serve(t, func(r *http.Request) ([]string, error) { return nil, errors.New("no credentials") })
	if code, _ := status(t, "GET", anon.URL+"/items/v1/items/7"); code != http.StatusUnauthorized {
		t.Fatalf("an authentication failure stays 401, got %d", code)
	}
	if code, _ := status(t, "GET", anon.URL+"/items/v1/ping"); code != http.StatusOK {
		t.Fatalf("the granted callback is not consulted for open routes, got %d", code)
	}
}

func TestMetadataCarriesScopes(t *testing.T) {
	var seen []string
	mux := http.NewServeMux()
	err := RegisterItemsServer(mux, items{}, WithAuthorizer(func(ctx context.Context, m RequestMetadata, r *http.Request) error {
		seen = append(seen, m.Method+"="+strings.Join(m.Scopes, ","))
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	status(t, "DELETE", server.URL+"/items/v1/items/7")
	status(t, "GET", server.URL+"/items/v1/ping")
	if len(seen) != 2 || seen[0] != "remove=items:read,items:write" || seen[1] != "ping=" {
		t.Fatalf("metadata = %v", seen)
	}
}
`)
}
