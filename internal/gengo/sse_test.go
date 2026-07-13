package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const sseFixtureSrc = `
package main

message StreamEventsRequest {
  channel: string @query("channel")
}

message Event {
  id: string
  payload: string
}

message StreamError @status(400) {
  reason: string
}

service SSEService {
  base_path: "/api/v1"

  streamEvents(StreamEventsRequest) -> Event | StreamError @get("/events") @stream
}
`

const sseHarnessMain = `
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
)

type sseImpl struct{}

func (s *sseImpl) StreamEvents(ctx context.Context, req *StreamEventsRequest, sender SSESender) error {
	switch req.Channel {
	case "fail-early":
		return &StreamError{Reason: "early failure"}
	case "fail-late":
		if err := sender.Send(&Event{Id: "1", Payload: "first"}); err != nil {
			return err
		}
		return fmt.Errorf("boom")
	default:
		for i := 1; i <= 3; i++ {
			if err := sender.Send(&Event{Id: fmt.Sprintf("%d", i), Payload: "hello"}); err != nil {
				return err
			}
		}
		return nil
	}
}

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	mux := http.NewServeMux()
	if err := RegisterSSEServiceServer(&sseImpl{}, WithMux(mux)); err != nil {
		fail("register: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := NewSSEServiceClient(srv.URL)

	stream, err := client.StreamEvents(ctx, &StreamEventsRequest{Channel: "ok"})
	if err != nil {
		fail("StreamEvents(ok) failed: %v", err)
	}
	var got []string
	event := &Event{}
	for stream.Next(event) {
		got = append(got, event.Id+":"+event.Payload)
	}
	if err := stream.Err(); err != nil {
		fail("unexpected stream error: %v", err)
	}
	stream.Close()
	if len(got) != 3 || got[0] != "1:hello" || got[2] != "3:hello" {
		fail("unexpected events: %+v", got)
	}

	_, err = client.StreamEvents(ctx, &StreamEventsRequest{Channel: "fail-early"})
	if err == nil {
		fail("expected error for fail-early, got nil")
	}
	streamErr, ok := err.(*StreamError)
	if !ok {
		fail("expected *StreamError, got %T: %v", err, err)
	}
	if streamErr.Reason != "early failure" {
		fail("unexpected StreamError: %+v", streamErr)
	}

	lateStream, err := client.StreamEvents(ctx, &StreamEventsRequest{Channel: "fail-late"})
	if err != nil {
		fail("StreamEvents(fail-late) failed: %v", err)
	}
	var lateGot []string
	lateEvent := &Event{}
	for lateStream.Next(lateEvent) {
		lateGot = append(lateGot, lateEvent.Id+":"+lateEvent.Payload)
	}
	if lateStream.Err() == nil {
		fail("expected late stream error, got nil")
	}
	lateStream.Close()
	if len(lateGot) != 1 || lateGot[0] != "1:first" {
		fail("unexpected late events: %+v", lateGot)
	}

	fmt.Println("OK")
}
`

func TestSSEStreamingEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	ast, err := onklang.Parse(sseFixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v", err)
	}
	validateSrc, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v", err)
	}
	serverSrc, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v\n%s", err, serverSrc)
	}
	clientSrc, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("GenerateClient error: %v\n%s", err, clientSrc)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_sse_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	if len(validateSrc) > 0 {
		writeFile(t, filepath.Join(dir, "validate.go"), string(validateSrc))
	}
	writeFile(t, filepath.Join(dir, "server.go"), string(serverSrc))
	writeFile(t, filepath.Join(dir, "client.go"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "main.go"), sseHarnessMain)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
