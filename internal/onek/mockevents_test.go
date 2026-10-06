package onek

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const mockEventsSchema = `package app

message Text { text: string }
message Done { reason: string }
message Turn {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
    done: Done @tag("done")
  }
}
message TurnRequest { prompt: string }

service Agent {
  turn(TurnRequest) -> Turn @post("/turn") @stream
}
`

func TestMockStreamNamesFramesAfterTheOneofVariant(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir+"/svc.onk", mockEventsSchema)
	server, err := NewMockServer(dir, MockOptions{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	response, err := http.Post(ts.URL+"/turn", "application/json", strings.NewReader(`{"prompt":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if got := strings.Count(string(body), "event: text\ndata: "); got != 3 {
		t.Fatalf("expected 3 named frames, got %d in:\n%s", got, body)
	}
}
