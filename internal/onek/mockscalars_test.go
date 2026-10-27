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
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Req {
  id: string;
}

model Res {
  counts: Record<int64>;
  sizes: Record<uint64>;
  seen: Record<utcDateTime>;
  blobs: Record<bytes>;
}

interface Svc {
  @get
  @route("/r/{id}")
  get(...Req): Res;
}
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
