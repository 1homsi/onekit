package onek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockFlattenedFieldsUseTheirPrefix(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Req {
  id: string;
}

model Addr {
  city: string;
}

model Res {
  id: string;
  @Onkt.flatten("billing_")
  billing: Addr;
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
	if body["billing_city"] != mockStringValue || body["billing"] != nil {
		t.Fatalf("flattened fixture: %v", body)
	}
}
