package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const streamErrorSchema = `package app

message Req { id: string }
message Tick { n: int32 }

service Feed {
  watch(Req) -> Tick @get("/watch") @stream
}
`

const streamErrorHarness = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	app "example.com/streamerror/app"
)

type quota struct{}

func (quota) Error() string        { return "quota exhausted for tenant 7" }
func (quota) HTTPStatusCode() int  { return http.StatusTooManyRequests }
func (quota) PublicCode() string   { return "quota_exceeded" }
func (quota) PublicMessage() string { return "daily quota reached" }

type impl struct{ fail error }

func (i impl) Watch(ctx context.Context, req *app.Req, sender app.SSESender) error {
	if err := sender.Send(&app.Tick{N: 1}); err != nil {
		return err
	}
	return i.fail
}

func read(fail error, opts ...any) string {
	mux := http.NewServeMux()
	if err := app.RegisterFeedServer(mux, append([]any{impl{fail: fail}}, opts...)...); err != nil { panic(err) }
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := http.Get(server.URL + "/watch")
	if err != nil { panic(err) }
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}

func main() {
	envelope := func(w http.ResponseWriter, r *http.Request, e *app.ServerError) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.Status)
		fmt.Fprintf(w, "{\"error\":{\"code\":%q,\"message\":%q}}", e.Code, e.Message)
	}
	got := read(quota{}, app.WithErrorWriter(envelope))
	want := "event: error\ndata: {\"error\":{\"code\":\"quota_exceeded\",\"message\":\"daily quota reached\"}}\n\n"
	if !strings.Contains(got, want) { panic("custom envelope with the handler's code and message:\n" + got) }

	got = read(errors.New("db password is hunter2"), app.WithErrorWriter(envelope))
	if strings.Contains(got, "hunter2") || !strings.Contains(got, "\"code\":\"internal\",\"message\":\"internal server error\"") {
		panic("an unexpected error must stay generic:\n" + got)
	}

	got = read(quota{})
	if !strings.Contains(got, "event: error\ndata: {\"message\":\"daily quota reached\"}") { panic("default writer keeps the public message:\n" + got) }
	fmt.Println("OK")
}
`

func TestGoMidStreamErrorsUseTheConfiguredErrorWriter(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, streamErrorSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/streamerror\n\ngo 1.26\n")
	for name, generate := range map[string]func() ([]byte, error){
		"types.go":    func() ([]byte, error) { return GenerateTypesWithResolver(file, nil) },
		"validate.go": func() ([]byte, error) { return GenerateValidationWithResolver(file, nil) },
		"server.go":   func() ([]byte, error) { return GenerateServerWithResolver(file, nil) },
	} {
		out, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", name), string(out))
	}
	writeFile(t, filepath.Join(dir, "main.go"), streamErrorHarness)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("%v\n%s", err, out)
	}
}
