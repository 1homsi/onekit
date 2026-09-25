package onek

import "testing"

func TestGoServerReturnsViolationList(t *testing.T) {
	buildGoSchema(t, `
package check

message Signup { email: string @email  name: string @len(2, 10) }

service Accounts { signup(Signup) -> Signup @post("/signup") }
`, `package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type accounts struct{}

func (accounts) Signup(ctx context.Context, req *Signup) (*Signup, error) { return req, nil }

func TestViolations(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterAccountsServer(mux, accounts{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/signup", "application/json", strings.NewReader(`+"`"+`{"email":"nope","name":"x"}`+"`"+`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Message    string   `+"`json:\"message\"`"+`
		Violations []string `+"`json:\"violations\"`"+`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 || len(body.Violations) != 2 || !strings.Contains(body.Message, "; ") {
		t.Fatalf("unexpected response %d %+v", resp.StatusCode, body)
	}
	var list *ValidationErrors
	if err := (&Signup{Email: "nope", Name: "x"}).Validate(); err == nil || !asViolations(err, &list) || len(list.Violations) != 2 {
		t.Fatalf("Validate did not return *ValidationErrors: %v", err)
	}
}

func asViolations(err error, target **ValidationErrors) bool {
	v, ok := err.(*ValidationErrors)
	*target = v
	return ok
}
`)
}
