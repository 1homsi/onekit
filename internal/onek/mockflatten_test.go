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
	writeTestFile(t, filepath.Join(dir, "api.onk"), `
message Req { id: string }
message Addr { city: string }
message Res {
  id: string
  billing: Addr @flatten(prefix: "billing_")
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
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["billing_city"] != "string" || body["billing"] != nil {
		t.Fatalf("flattened fixture: %v", body)
	}
}
