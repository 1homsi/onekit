package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const wsFixtureSrc = `
package wsf

message ChatMessage { room: string text: string }
message ChatEvent { seq: int64 text: string }

service ChatService {
  base_path: "/v1"

  chat(ChatMessage) -> ChatEvent @ws("/rooms/{room}")
}
`

func compileWSFile(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(wsFixtureSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "ws.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

const wsCorrelatedFixtureSrc = `
package wsc

message RunRequest { code: string }
message RunResult { exit_code: int32 }
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
  }
}

service Runtime {
  base_path: "/v1"

  execute(Frame) -> Frame @ws("/execute")
}
`

func compileWSCorrelatedFile(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(wsCorrelatedFixtureSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "wsc.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func TestGenerateServerWebSocketsCorrelated(t *testing.T) {
	file := compileWSCorrelatedFile(t)
	text, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	out := string(text)
	for _, want := range []string{
		"type wsServerPending[K comparable, T any] struct {",
		"type RuntimeExecuteOut struct {",
		"*wsServerPending[string, *Frame]",
		"func (o *RuntimeExecuteOut) Call(ctx context.Context, id string, value *Frame) (*Frame, error) {",
		"Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error",
		"newWSServerPending[string, *Frame]()",
		"out.pending.resolve(replyID, frame)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated correlated server missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateClientWebSocketsCorrelated(t *testing.T) {
	file := compileWSCorrelatedFile(t)
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("generate client: %v", err)
	}
	out := string(client)
	for _, want := range []string{
		"type wsPending[K comparable, T any] struct {",
		"*wsPending[string, *Frame]",
		"func (d *FrameToFrameSocket) Call(ctx context.Context, id string, value *Frame) (*Frame, error) {",
		"func (d *FrameToFrameSocket) readLoop() {",
		"d.pending.resolve(id, frame)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated correlated client missing %q:\n%s", want, out)
		}
	}
}

// TestGeneratedWSCorrelatedServerCompiles pins that a @ws_id-using server AND
// client build against a real module - this generics/goroutine/channel code
// is exactly the shape substring assertions above are weakest at catching.
func TestGeneratedWSCorrelatedServerCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileWSCorrelatedFile(t)
	server, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("generate client: %v", err)
	}
	types, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("generate validation: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":          "module wsc\n\ngo 1.24\n\nrequire github.com/coder/websocket v1.8.15\n",
		"server.go":       string(server),
		"client.go":       string(client),
		"types.gen.go":    string(types),
		"validate.gen.go": string(validation),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated correlated WS client/server failed to build: %v\n%s", err, out)
	}
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("generated correlated WS client/server failed to vet: %v\n%s", err, out)
	}
}

func TestGenerateServerWebSockets(t *testing.T) {
	file := compileWSFile(t)
	text, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}
	out := string(text)
	for _, want := range []string{
		`"github.com/coder/websocket"`,
		"type WSOut[E any] interface {",
		"Chat(ctx context.Context, req *ChatMessage, out WSOut[ChatEvent]) error",
		`mux.Handle("GET /v1/rooms/{room}"`,
		"websocket.Accept(w, r, nil)",
		"wsConnOut[ChatEvent]{conn: conn}",
		"s.mu.Lock()",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated server missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateClientWebSockets(t *testing.T) {
	file := compileWSFile(t)
	client, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("generate client: %v", err)
	}
	out := string(client)
	for _, want := range []string{
		`"github.com/coder/websocket"`,
		"type ChatMessageToChatEventSocket struct {",
		"mu   sync.Mutex",
		"d.mu.Lock()",
		") (*ChatMessageToChatEventSocket, error) {",
		// Without the base URL the client dials a bare path and every call
		// fails with `unexpected url scheme: ""`, the same as unary and SSE.
		"socketURL := c.BaseURL + path",
		`socketURL = "wss://" + strings.TrimPrefix(socketURL, "https://")`,
		"&websocket.DialOptions{HTTPClient: c.HTTPClient, HTTPHeader: header}",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated client missing %q:\n%s", want, out)
		}
	}
}

// TestGeneratedWSServerCompiles pins that emitted WebSocket servers build
// against a real module (coder/websocket resolves via the module proxy).
func TestGeneratedWSServerCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileWSFile(t)
	server, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	types, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("generate validation: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":          "module wsf\n\ngo 1.24\n\nrequire github.com/coder/websocket v1.8.15\n",
		"server.go":       string(server),
		"types.gen.go":    string(types),
		"validate.gen.go": string(validation),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated WS server failed to build: %v\n%s", err, out)
	}
}
