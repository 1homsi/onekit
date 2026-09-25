package onek

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMockReloadPicksUpSchemaChangesAndSurvivesBrokenEdits(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "api.onk")
	write := func(src string) {
		t.Helper()
		writeTestFile(t, schema, src)
		later := time.Now().Add(time.Second)
		if err := os.Chtimes(schema, later, later); err != nil {
			t.Fatal(err)
		}
	}
	write(`message Req {}
message Res { id: string }
service S { get(Req) -> Res @get("/a") }
`)
	reloader, routes, err := newMockReloader(dir, MockOptions{})
	if err != nil || routes != 1 {
		t.Fatalf("routes = %d, err = %v", routes, err)
	}
	status := func(path string) int {
		recorder := httptest.NewRecorder()
		reloader.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder.Code
	}
	if status("/a") != http.StatusOK || status("/b") != http.StatusNotFound {
		t.Fatal("initial routes wrong")
	}
	var out bytes.Buffer
	write(`message Req {}
message Res { id: string }
service S {
  get(Req) -> Res @get("/a")
  other(Req) -> Res @get("/b")
}
`)
	reloader.reload(&out)
	if status("/b") != http.StatusOK || !strings.Contains(out.String(), "reloaded 2 route(s)") {
		t.Fatalf("reload missed the new route: %s", out.String())
	}
	out.Reset()
	write("message Broken {\n")
	reloader.reload(&out)
	if status("/b") != http.StatusOK || !strings.Contains(out.String(), "still serving the previous schema") {
		t.Fatalf("broken schema replaced the mock: %s", out.String())
	}
}
