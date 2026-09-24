package onek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockFixturesFillEveryMapValueScalar(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), `
message Req { id: string }
message Res {
  counts: map[string, int64]
  sizes: map[string, uint64]
  seen: map[string, timestamp]
  blobs: map[string, bytes]
}
service Svc { get(Req) -> Res @get("/r/{id}") }
`)
	server, err := NewMockServer(dir, MockOptions{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/r/1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"counts": "1729", "sizes": "1729", "seen": "2025-01-01T00:00:00Z", "blobs": "YQ=="}
	for field, value := range want {
		if body[field]["key"] != value {
			t.Fatalf("%s map value = %v, want %v (body %v)", field, body[field]["key"], value, body)
		}
	}
}
