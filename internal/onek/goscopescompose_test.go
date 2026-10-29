package onek

import "testing"

// WithScopes and WithAuthorizer both apply to a route: the scope check runs
// first, the Authorizer only when it passes, and both run before the body is
// decoded. The scope callback is not called for routes without @requires,
// and a missing scope reaches the error writer as Code "forbidden".
func TestGoServerComposesScopesWithTheAuthorizer(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Item {
  id: string;
}

model SaveItem {
  id: string;
  @minLength(3)
  name: string;
}

model Nothing {}

@route("/items")
interface Items {
  @post
  @route("/{id}")
  @Onkt.requires("admin")
  save(...SaveItem): Item;

  @get
  @route("/ping")
  ping(...Nothing): Nothing;
}
`, `package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type items struct{}

func (items) Save(ctx context.Context, req *SaveItem) (*Item, error) { return &Item{Id: req.Id}, nil }
func (items) Ping(ctx context.Context, req *Nothing) (*Nothing, error) { return &Nothing{}, nil }

var errNoObject = &objectError{}

type objectError struct{}

func (*objectError) Error() string       { return "no access to this object" }
func (*objectError) HTTPStatusCode() int { return http.StatusForbidden }

func TestCompose(t *testing.T) {
	var calls []string
	var scopes []string
	authorized := true
	var written []string
	mux := http.NewServeMux()
	err := RegisterItemsServer(mux, items{},
		WithAuthorizer(func(ctx context.Context, m RequestMetadata, r *http.Request) error {
			calls = append(calls, "authorizer:"+m.Method)
			if !authorized {
				return errNoObject
			}
			return nil
		}),
		WithScopes(func(ctx context.Context, r *http.Request) ([]string, error) {
			m, _ := RequestMetadataFromContext(ctx)
			calls = append(calls, "scopes:"+strings.Join(m.Scopes, ","))
			return scopes, nil
		}),
		WithErrorWriter(func(w http.ResponseWriter, r *http.Request, e *ServerError) {
			var scopeErr *ScopeError
			missing := ""
			if errors.As(e.Cause, &scopeErr) {
				missing = strings.Join(scopeErr.Missing, ",")
			}
			written = append(written, e.Code+":"+missing)
			w.WriteHeader(e.Status)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	post := func(body string) int {
		resp, err := http.Post(server.URL+"/items/7", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	reset := func() { calls, written = nil, nil }

	if code := post("{not json"); code != http.StatusForbidden || strings.Join(calls, " ") != "scopes:admin" || strings.Join(written, " ") != "forbidden:admin" {
		t.Fatalf("a caller without the scope gets 403 before the body is read: %d %v %v", code, calls, written)
	}

	reset()
	scopes = []string{"admin"}
	authorized = false
	if code := post("{not json"); code != http.StatusForbidden || strings.Join(calls, " ") != "scopes:admin authorizer:save" || strings.Join(written, " ") != "forbidden:" {
		t.Fatalf("the authorizer runs after the scope check passes, before decoding: %d %v %v", code, calls, written)
	}

	reset()
	authorized = true
	if code := post("{not json"); code != http.StatusBadRequest || strings.Join(written, " ") != "invalid_request_body:" {
		t.Fatalf("an authorized caller then gets the decode error: %d %v", code, written)
	}

	reset()
	if code := post(`+"`"+`{"name":"widget"}`+"`"+`); code != http.StatusOK {
		t.Fatalf("both checks pass: %d %v", code, written)
	}

	reset()
	resp, err := http.Get(server.URL + "/items/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Join(calls, " ") != "authorizer:ping" {
		t.Fatalf("a route without @requires does not ask for scopes: %d %v", resp.StatusCode, calls)
	}
}
`)
}
