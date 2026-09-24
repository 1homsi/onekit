package onek

import "testing"

func TestGoAuthorizerRunsInsideMiddlewareAndObserver(t *testing.T) {
	buildGoSchema(t, `
package check

message Ping { id: string }

service Pings { get(Ping) -> Ping @get("/pings/{id}") }
`, `package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pings struct{}

func (pings) Get(ctx context.Context, req *Ping) (*Ping, error) { return req, nil }

type observer struct{ statuses []int }

func (o *observer) RequestStarted(ctx context.Context, _ RequestMetadata) context.Context { return ctx }
func (o *observer) RequestFinished(_ context.Context, _ RequestMetadata, result RequestResult) {
	o.statuses = append(o.statuses, result.StatusCode)
}

func TestAuthorizerOrder(t *testing.T) {
	seen := 0
	obs := &observer{}
	mux := http.NewServeMux()
	err := RegisterPingsServer(mux, pings{},
		WithMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen++; next.ServeHTTP(w, r) })
		}),
		WithRequestObserver(obs),
		WithAuthorizer(func(ctx context.Context, md RequestMetadata, r *http.Request) error {
			if _, ok := RequestMetadataFromContext(ctx); !ok {
				return errors.New("metadata missing")
			}
			return errors.New("denied")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/pings/1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || seen != 1 || len(obs.statuses) != 1 || obs.statuses[0] != http.StatusUnauthorized {
		t.Fatalf("status %d middleware %d observer %v", resp.StatusCode, seen, obs.statuses)
	}
}
`)
}
