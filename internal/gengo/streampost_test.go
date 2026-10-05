package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const streamPostSchema = `package app

message TurnRequest { prompt: string }
message Text { text: string }
message Done { reason: string }
message TurnEvent {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
    done: Done @tag("done")
  }
}

service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream
}
`

const streamPostHarness = `package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	app "example.com/streampost/app"
)

type impl struct{}

func (impl) Turn(ctx context.Context, req *app.TurnRequest, sender app.SSESender) error {
	time.Sleep(300 * time.Millisecond)
	if err := sender.Send(&app.TurnEvent{Payload: &app.TurnEventPayloadText{Text: &app.Text{Text: "hi " + req.Prompt}}}); err != nil {
		return err
	}
	return sender.Send(&app.TurnEvent{Payload: &app.TurnEventPayloadDone{Done: &app.Done{Reason: "stop"}}})
}

func main() {
	mux := http.NewServeMux()
	if err := app.RegisterAgentServer(mux, impl{}, app.WithSSEHeartbeat(50*time.Millisecond)); err != nil {
		panic(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	start := time.Now()
	response, err := http.Post(server.URL+"/turn", "application/json", strings.NewReader("{\"prompt\":\"bob\"}"))
	if err != nil { panic(err) }
	if time.Since(start) > 250*time.Millisecond {
		panic("heartbeat must commit headers before the first slow event")
	}
	var lines []string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if scanner.Text() != "" && !strings.HasPrefix(scanner.Text(), ":") {
			lines = append(lines, scanner.Text())
		}
	}
	response.Body.Close()
	want := "[event: text data: {\"payload\":{\"type\":\"text\",\"text\":{\"text\":\"hi bob\"}}} event: done data: {\"payload\":{\"type\":\"done\",\"done\":{\"reason\":\"stop\"}}}]"
	if got := fmt.Sprint(lines); got != want {
		panic(fmt.Sprintf("wire:\n%s\nwant\n%s", got, want))
	}

	client := app.NewAgentClient(server.URL)
	stream, err := client.Turn(context.Background(), &app.TurnRequest{Prompt: "amy"})
	if err != nil { panic(err) }
	defer stream.Close()
	var seen []string
	var event app.TurnEvent
	for stream.Next(&event) {
		switch v := event.Payload.(type) {
		case *app.TurnEventPayloadText:
			seen = append(seen, "text:"+v.Text.Text)
		case *app.TurnEventPayloadDone:
			seen = append(seen, "done:"+v.Done.Reason)
		}
		event = app.TurnEvent{}
	}
	if stream.Err() != nil { panic(stream.Err()) }
	if fmt.Sprint(seen) != "[text:hi amy done:stop]" { panic(fmt.Sprint(seen)) }
	fmt.Println("OK")
}
`

func TestGoPostStreamWithTypedEventsAndHeartbeat(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, streamPostSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/streampost\n\ngo 1.26\n")
	for name, generate := range map[string]func() ([]byte, error){
		"types.go":    func() ([]byte, error) { return GenerateTypesWithResolver(file, nil) },
		"validate.go": func() ([]byte, error) { return GenerateValidationWithResolver(file, nil) },
		"server.go":   func() ([]byte, error) { return GenerateServerWithResolver(file, nil) },
		"client.go":   func() ([]byte, error) { return GenerateClientWithResolver(file, nil) },
	} {
		out, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", name), string(out))
	}
	writeFile(t, filepath.Join(dir, "main.go"), streamPostHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
