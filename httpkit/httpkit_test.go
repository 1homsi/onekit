package httpkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeError struct {
	status               int
	code, message, field string
	violations           []string
	cause                error
}

func (e *fakeError) Parts() (int, string, string, string, []string, error) {
	return e.status, e.code, e.message, e.field, e.violations, e.cause
}

func TestEnvelopeWritesTheErrorShapeAndLogsOnlyServerErrors(t *testing.T) {
	var logs bytes.Buffer
	write := ErrorWriter[*fakeError](Envelope{Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	handler := Middleware(Config{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write(w, r, &fakeError{status: 500, code: "internal", message: "internal server error", cause: errors.New("db password leaked")})
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "req-1")
	handler.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 500 || body.Error.Code != "internal" || body.Error.RequestID != "" && body.Error.RequestID != "req-1" {
		t.Fatalf("unexpected response %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"req-1"`) {
		t.Fatalf("the request id must be in the envelope: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("the cause must never reach the client: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "db password leaked") || !strings.Contains(logs.String(), "req-1") {
		t.Fatalf("a 5xx cause must be logged with the request id: %s", logs.String())
	}

	logs.Reset()
	rec = httptest.NewRecorder()
	write(rec, httptest.NewRequest(http.MethodGet, "/x", nil), &fakeError{status: 400, code: "invalid_query_parameter", message: "bad", field: "limit", violations: []string{"a", "b"}, cause: errors.New("strconv")})
	if logs.Len() != 0 {
		t.Fatalf("4xx failures are not logged: %s", logs.String())
	}
	if !strings.Contains(rec.Body.String(), `"field":"limit"`) || !strings.Contains(rec.Body.String(), `"violations":["a","b"]`) {
		t.Fatalf("field and violations belong in the envelope: %s", rec.Body)
	}
}

func TestEnvelopeFallsBackToTheResponseHeaderAndAllowsCustomContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-ID", "from-header")
	Envelope{ContentType: "application/json; charset=utf-8"}.Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), Failure{Status: 404, Code: "not_found", Message: "no"})
	if !strings.Contains(rec.Body.String(), `"request_id":"from-header"`) || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("%v %s", rec.Header(), rec.Body)
	}
}

func TestMiddlewareProvidesStateAndRecordsTheResponse(t *testing.T) {
	type user struct{ ID int }
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var seen *State
	handler := Middleware(Config{Now: func() time.Time { return now }, NewRequestID: func() string { return "generated" }})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = StateFrom(r.Context())
		SetPrincipal(r.Context(), user{ID: 7})
		if MustPrincipal[user](r.Context()).ID != 7 {
			t.Error("principal lost")
		}
		if _, ok := Principal[string](r.Context()); ok {
			t.Error("a principal of another type must not match")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "192.0.2.9:4000"
	handler.ServeHTTP(rec, req)
	if seen.RequestID != "generated" || rec.Header().Get("X-Request-ID") != "generated" || !seen.Start.Equal(now) || seen.ClientIP != "192.0.2.9" {
		t.Fatalf("state = %+v", seen)
	}
	if seen.Status() != 201 || seen.Bytes() != 5 {
		t.Fatalf("recorded %d / %d", seen.Status(), seen.Bytes())
	}
	if p, ok := Principal[user](WithState(context.Background(), seen)); !ok || p.ID != 7 {
		t.Fatal("the principal must stay readable after the handler returned")
	}
}

func TestMustPrincipalPanicsWhenNoneIsSet(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	MustPrincipal[string](context.Background())
}

func TestClientIPHonorsForwardedForOnlyFromTrustedProxies(t *testing.T) {
	trusted := ParseProxies([]string{"10.0.0.0/8", "192.0.2.1"})
	cases := []struct {
		name, remote, forwarded, want string
	}{
		{"direct peer", "198.51.100.7:1", "1.2.3.4", "198.51.100.7"},
		{"trusted proxy", "10.1.2.3:1", "203.0.113.5", "203.0.113.5"},
		{"chain skips trusted hops", "10.1.2.3:1", "203.0.113.5, 10.9.9.9, 192.0.2.1", "203.0.113.5"},
		{"client cannot spoof past a trusted hop", "10.1.2.3:1", "1.1.1.1, 203.0.113.5", "203.0.113.5"},
		{"garbage falls back to the peer", "10.1.2.3:1", "not-an-ip", "10.1.2.3"},
		{"no header", "10.1.2.3:1", "", "10.1.2.3"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = c.remote
		if c.forwarded != "" {
			req.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := ClientIP(req, trusted); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestResolveGuardFillsPathValues(t *testing.T) {
	values := map[string]string{"id": "42", "org": "acme"}
	get := func(name string) string { return values[name] }
	got, ok := ResolveGuards([]string{"object/level/:id", "org/:org/admin", "static"}, get)
	if !ok || strings.Join(got, ",") != "object/level/42,org/acme/admin,static" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := ResolveGuard("object/:missing", get); ok {
		t.Fatal("a placeholder without a value must not resolve")
	}
}

func TestRecorderStandaloneAndFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	r := NewRecorder(rec)
	_, _ = r.Write([]byte("abc"))
	r.Flush()
	if r.Status() != 200 || r.Bytes() != 3 || !rec.Flushed {
		t.Fatalf("%d %d %v", r.Status(), r.Bytes(), rec.Flushed)
	}
}
