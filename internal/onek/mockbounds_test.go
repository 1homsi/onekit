package onek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockNumbersRespectValidators(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Req {
  id: string;
}

model Res {
  @Onkt.range(1, 10)
  page: int32;
  @minValueExclusive(100)
  count: uint32;
  @maxValueExclusive(1)
  ratio: float64;
  @maxValue(99)
  @encode(string)
  total: int64;
  plain: int32;
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
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["page"] != 10.0 || body["count"] != 101.0 || body["ratio"] != 0.5 || body["total"] != "99" || body["plain"] != 42.0 {
		t.Fatalf("fixtures violate validators: %v", body)
	}
}
