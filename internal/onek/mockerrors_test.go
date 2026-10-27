package onek

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockInjectsEveryDeclaredErrorType(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Req {
  id: string;
}

model Res {
  id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

@error
model Conflict {
  @statusCode _: 409;
  message: string;
}

interface Svc {
  @get
  @route("/r/{id}")
  get(...Req): Res | NotFound | Conflict;
}
`)
	server, err := NewMockServer(dir, MockOptions{Seed: 3, ErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	seen := map[int]bool{}
	for range 40 {
		resp, err := http.Get(ts.URL + "/r/1")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		seen[resp.StatusCode] = true
	}
	if !seen[404] || !seen[409] || len(seen) != 2 {
		t.Fatalf("statuses seen: %v", seen)
	}
}
