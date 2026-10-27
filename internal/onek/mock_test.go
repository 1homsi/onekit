package onek

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const mockSchema = `namespace probe;

model Req {
  @format("uuid")
  id: string;
}

model Res {
  ok: boolean;
  @format("email")
  who: string;
  @encode(string)
  big: int64;
  @encode("unixTimestamp", int64)
  when_ts: utcDateTime;
}

@error
model Missing {
  @statusCode _: 404;
  message: string;
}

@route("/v1")
interface Svc {
  @get
  @route("/things/{id}")
  getThing(...Req): Res | Missing;

  @Onkt.method("QUERY")
  @route("/things")
  findThing(...Req): Res;

  @get
  @route("/things/stream")
  @Onkt.stream
  streamThings(...Req): Res;
}
`

func newMockTestServer(t *testing.T, opts MockOptions) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, dir+"/svc.tsp", mockSchema)
	server, err := NewMockServer(dir, opts)
	if err != nil {
		t.Fatalf("NewMockServer: %v", err)
	}
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestMockServerServesValidatorAccurateFixtures(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 1})
	resp, err := http.Get(ts.URL + "/v1/things/abc")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		OK     bool   `json:"ok"`
		Who    string `json:"who"`
		Big    any    `json:"big"`
		WhenTS any    `json:"when_ts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK || body.Who != mockEmail || body.Big != "1729" || body.WhenTS != float64(1735689600) {
		t.Fatalf("unexpected fixture: %+v", body)
	}
}

func TestMockServerErrorInjectionUsesDeclaredStatus(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 1, ErrorRate: 1})
	for range 3 {
		resp, err := http.Get(ts.URL + "/v1/things/abc")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %s)", resp.StatusCode, data)
		}
	}
}

func TestMockServerDeterministicPerSeed(t *testing.T) {
	a := newMockTestServer(t, MockOptions{Seed: 7})
	b := newMockTestServer(t, MockOptions{Seed: 7})
	first, err := http.Get(a.URL + "/v1/things/x")
	if err != nil {
		t.Fatalf("GET a: %v", err)
	}
	second, err := http.Get(b.URL + "/v1/things/x")
	if err != nil {
		t.Fatalf("GET b: %v", err)
	}
	var bufA, bufB strings.Builder
	_, _ = io.Copy(&bufA, first.Body)
	_, _ = io.Copy(&bufB, second.Body)
	first.Body.Close()
	second.Body.Close()
	if bufA.String() == "" || bufA.String() != bufB.String() {
		t.Fatalf("fixtures must be byte-identical per seed:\n%s\n---\n%s", bufA.String(), bufB.String())
	}
}

func TestMockServerSupportsQueryVerbAndStreams(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 3})

	req, err := http.NewRequest("QUERY", ts.URL+"/v1/things", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("QUERY: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("QUERY status = %d", resp.StatusCode)
	}

	streamResp, err := http.Get(ts.URL + "/v1/things/stream")
	if err != nil {
		t.Fatalf("stream GET: %v", err)
	}
	defer streamResp.Body.Close()
	if ct := streamResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	data, _ := io.ReadAll(streamResp.Body)
	if frames := strings.Count(string(data), "data: "); frames != 3 {
		t.Fatalf("expected 3 SSE frames, got %d in:\n%s", frames, data)
	}
}

func TestMockServerRejectsBadErrorRate(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir+"/svc.tsp", mockSchema)
	if _, err := NewMockServer(dir, MockOptions{ErrorRate: 1.5}); err == nil {
		t.Fatal("expected error-rate validation failure")
	}
}
