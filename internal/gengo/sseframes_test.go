package gengo

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sseFramesSchema = `namespace app;

model Req {
  room: string;
}

model Tick {
  n: int32;
  doc: unknown;
  big: string;
  note: string;
}

interface Feed {
  @get
  @route("/watch")
  @Onkt.stream
  watch(...Req): Tick;
}
`

const sseFramesHarness = `package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type feed struct{}

func (feed) Watch(ctx context.Context, req *Req, sender SSESender) error {
	docs := []string{
		"{\n  \"a\": 1,\r\n  \"b\": [1,\n2]\n}",
		"{\"plain\":true}",
		"[1,\r2,\r\n3]",
	}
	for i, doc := range docs {
		if err := sender.Send(&Tick{N: int32(i), Doc: json.RawMessage(doc), Big: strings.Repeat("x", 20000), Note: "line1\nline2\r\nline3"}); err != nil {
			return err
		}
	}
	for i := 0; i < 50; i++ {
		if err := sender.SendWithEvent("tick", &Tick{N: int32(100 + i), Note: "n"}); err != nil {
			return err
		}
	}
	return nil
}

func TestSSEFraming(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterFeedServer(mux, feed{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stream, err := NewFeedClient(srv.URL).Watch(context.Background(), &Req{Room: "r"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var tick Tick
	seen := 0
	for stream.Next(&tick) {
		if tick.N < 3 {
			var compact bytes.Buffer
			if err := json.Compact(&compact, tick.Doc); err != nil {
				t.Fatalf("event %d: the json field is not valid JSON after a round trip: %v %q", tick.N, err, tick.Doc)
			}
			want := []string{` + "`{\"a\":1,\"b\":[1,2]}`, `{\"plain\":true}`, `[1,2,3]`" + `}[tick.N]
			if compact.String() != want {
				t.Fatalf("event %d: got %q want %q", tick.N, compact.String(), want)
			}
			if len(tick.Big) != 20000 {
				t.Fatalf("event %d: a line longer than the read buffer lost data: %d bytes", tick.N, len(tick.Big))
			}
			if tick.Note != "line1\nline2\r\nline3" {
				t.Fatalf("event %d: escaped newlines in a string must survive: %q", tick.N, tick.Note)
			}
		}
		seen++
	}
	if stream.Err() != nil || seen != 53 {
		t.Fatalf("saw %d events, err %v", seen, stream.Err())
	}
}
`

func TestSSEFramesSurviveRawJSONAndLongLines(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, sseFramesSchema)
	types, err := GenerateTypes(file)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateServerWithOptions(file, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/sse\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.gen.go"), string(types))
	writeFile(t, filepath.Join(dir, "validate.gen.go"), string(validation))
	writeFile(t, filepath.Join(dir, "server.gen.go"), string(server))
	writeFile(t, filepath.Join(dir, "client.gen.go"), string(client))
	writeFile(t, filepath.Join(dir, "frames_test.go"), sseFramesHarness)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.TrimSpace(string(out)))
	}
}
