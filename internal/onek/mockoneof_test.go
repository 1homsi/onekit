package onek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockOneofFixturesMatchTheWireShape(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), `
message Req { id: string }
message EmailAuth { email: string @email }
message Res {
  nested: oneof(discriminator: "kind") { email: EmailAuth  code: string }
  flat: oneof(flatten: true) { email: EmailAuth }
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
	nested, _ := body["nested"]["email"].(map[string]any)
	if body["nested"]["kind"] != "email" || nested["email"] != mockEmail {
		t.Fatalf("nested oneof shape: %v", body["nested"])
	}
	if body["flat"]["type"] != "email" || body["flat"]["email"] != mockEmail {
		t.Fatalf("flattened oneof shape: %v", body["flat"])
	}
}
