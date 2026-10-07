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
	"testing/fstest"
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

func TestResolveGuardRefusesValuesThatWouldChangeTheKey(t *testing.T) {
	for _, value := range []string{"", ".", "..", "1/admin", "a\\b", "x\ny", "42\x00"} {
		get := func(string) string { return value }
		if resolved, ok := ResolveGuard("object/level/:id", get); ok {
			t.Errorf("%q must not resolve, got %q", value, resolved)
		}
	}
	if resolved, ok := ResolveGuard("object/level/:id", func(string) string { return "a b:c" }); !ok || resolved != "object/level/a b:c" {
		t.Errorf("ordinary values still resolve: %q %v", resolved, ok)
	}
}

func TestHealthReportsFailedChecksWithoutErrorText(t *testing.T) {
	handler := Health(Check{Name: "db", Run: func(context.Context) error { return errors.New("dial tcp 10.0.0.5: refused") }}, Check{Name: "cache"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"failed":["db"]`) || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	Health(Check{Name: "cache", Run: func(context.Context) error { return nil }}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestSPAServesFilesFallsBackToIndexAnd404sMissingAssets(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
		"robots.txt":        {Data: []byte("User-agent: *")},
	}
	var served []string
	handler := SPA(fsys, SPAOptions{OnServe: func(w http.ResponseWriter, _ *http.Request, name string) {
		served = append(served, name)
		w.Header().Set("X-Frame-Options", "DENY")
	}})
	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	if rec := get("/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("index: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if rec := get("/apps/42/settings"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<html>") {
		t.Fatalf("client route must get index.html: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/assets/app-abc.js"); rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed asset: %d %v", rec.Code, rec.Header())
	}
	if rec := get("/assets/missing.js"); rec.Code != 404 {
		t.Fatalf("a missing file with an extension is a 404, got %d", rec.Code)
	}
	if rec := get("/robots.txt"); rec.Code != 200 || rec.Body.String() != "User-agent: *" {
		t.Fatalf("plain file: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/../../etc/passwd"); rec.Code == 200 && strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("path traversal")
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/", nil))
	if post.Code != 405 {
		t.Fatalf("POST: %d", post.Code)
	}
	if len(served) == 0 {
		t.Fatal("OnServe must run")
	}
}

func TestETagAndIfNoneMatch(t *testing.T) {
	tag := ETag([]byte("bundle"))
	if !strings.HasPrefix(tag, `"`) || tag != ETag([]byte("bundle")) || tag == ETag([]byte("other")) {
		t.Fatalf("etag %q", tag)
	}
	for header, want := range map[string]bool{
		tag:              true,
		"W/" + tag:       true,
		`"nope", ` + tag: true,
		"*":              true,
		`"nope"`:         false,
		"":               false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("If-None-Match", header)
		}
		if got := IfNoneMatch(req, tag); got != want {
			t.Errorf("If-None-Match %q = %v, want %v", header, got, want)
		}
	}
}

func TestEnsureStateIsSharedWithMiddlewareAndFlagsAreVisibleToObservers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "outer")
	ctx, outer := EnsureState(req.Context(), req, Config{})
	var inner *State
	handler := Middleware(Config{})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		inner = StateFrom(r.Context())
		SetFlag(r.Context(), "skip-audit")
		SetPrincipal(r.Context(), "alice")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	if inner != outer || outer.RequestID != "outer" || !outer.Flag("skip-audit") || outer.Principal() != "alice" {
		t.Fatalf("the middleware must reuse the observer's state: %+v", outer)
	}
}
