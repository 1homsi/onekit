package onek

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const mockEventsSchema = `namespace app;

model Text {
  text: string;
}

model Done {
  reason: string;
}

@Onkt.discriminator("type")
union TurnPayload {
  @Onkt.tag("text") text: Text,
  @Onkt.tag("done") done: Done,
}

model Turn {
  payload: TurnPayload;
}

model TurnRequest {
  prompt: string;
}

interface Agent {
  @post
  @route("/turn")
  @Onkt.stream
  turn(...TurnRequest): Turn;
}
`

func TestMockStreamNamesFramesAfterTheOneofVariant(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir+"/svc.tsp", mockEventsSchema)
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
