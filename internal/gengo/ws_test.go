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
message Cancel { id: string @ws_id }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
    cancel: Cancel @tag("cancel") @ws_cancel
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
		"out.pending.resolve(replyID, replyVariant, frame)",
		// Both oneof variants carrying @ws_id must get extraction code, not
		// just whichever one happens to be first by declaration order (the
		// original bug: filtering by a single reference field's identity
		// only ever matched host_call, silently dropping host_result).
		"replyID, replyIDOk = v.HostCall.Id, true",
		"replyID, replyIDOk = v.HostResult.Id, true",
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
		"d.pending.resolve(id, variant, frame)",
		"id, idOk = v.HostCall.Id, true",
		"id, idOk = v.HostResult.Id, true",
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
		"wsConnOut[ChatEvent]{conn: conn, ctx: connCtx}",
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

// wsCorrelatedRuntimeHarness drives a real generated client and server over
// an actual WebSocket connection through the reference multiplexing
// scenario: the server answers the client's initial RunRequest by pushing a
// HostCall and awaiting the matching HostResult (a *different* oneof
// variant, with its own field) before sending RunResult. This is the exact
// shape a compile-only or single-variant test cannot catch: an earlier
// version of writeWSIDExtraction filtered oneof variants by comparing
// against a single reference @ws_id field's identity, which only ever
// matched whichever field onkir.WSIDField found first by declaration
// order (host_call) - so the server's read loop never recognized an
// incoming host_result frame as a reply, and Call() just hung until the
// misrouted frame reached the business handler instead.
const wsCorrelatedRuntimeHarnessPkg = "wsc"

var wsCorrelatedRuntimeHarness = `
package ` + wsCorrelatedRuntimeHarnessPkg + `

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type runtimeImpl struct{}

func (h *runtimeImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() {
		reply, err := out.Call(context.Background(), "call-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "call-1", Method: "doThing"}}})
		if err != nil {
			return
		}
		result := reply.GetHostResult()
		if result == nil || result.Id != "call-1" || result.Value != "answer" {
			return
		}
		_ = out.Send(context.Background(), &Frame{Payload: &FramePayloadRunResult{RunResult: &RunResult{ExitCode: 0}}})
	}()
	return nil
}

func TestRuntimeHarness(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRuntimeServer(mux, &runtimeImpl{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewRuntimeClient("http://" + server.Listener.Addr().String())
	socket, err := client.Execute(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "print(1)"}}})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer socket.Close()

	if err := socket.Send(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "print(1)"}}}); err != nil {
		t.Fatalf("send run request: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		frame, err := socket.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if call := frame.GetHostCall(); call != nil {
			reply := &Frame{Payload: &FramePayloadHostResult{HostResult: &HostResult{Id: call.Id, Value: "answer"}}}
			if err := socket.Send(context.Background(), reply); err != nil {
				t.Fatalf("send host result: %v", err)
			}
			continue
		}
		if frame.GetRunResult() != nil {
			return
		}
		t.Fatalf("unexpected frame: %+v", frame)
	}
	t.Fatal("timed out waiting for RunResult - Call() never got its HostResult reply")
}

type lateCallImpl struct{ result chan error }

func (h *lateCallImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := out.Call(context.Background(), "late-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "late-1", Method: "late"}}})
		h.result <- err
	}()
	return nil
}

// A Call made after the peer has gone must fail promptly - even with no
// deadline on ctx - rather than waiting on a reply that can never arrive.
func TestRuntimeLateCallAfterClose(t *testing.T) {
	impl := &lateCallImpl{result: make(chan error, 1)}
	mux := http.NewServeMux()
	if err := RegisterRuntimeServer(mux, impl); err != nil {
		t.Fatalf("register: %v", err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewRuntimeClient("http://" + server.Listener.Addr().String())
	socket, err := client.Execute(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := socket.Send(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send run request: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = socket.Close()

	select {
	case err := <-impl.result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Call after the peer closed: want net.ErrClosed, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Call after the peer closed never returned")
	}
}

func serveRuntime(t *testing.T, srv RuntimeServer, opts ...any) string {
	t.Helper()
	mux := http.NewServeMux()
	if err := RegisterRuntimeServer(mux, append([]any{srv}, opts...)...); err != nil {
		t.Fatalf("register: %v", err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func dialRuntime(t *testing.T, srv RuntimeServer, opts ...any) *FrameToFrameSocket {
	t.Helper()
	return dialRuntimeClient(t, NewRuntimeClient(serveRuntime(t, srv, opts...)))
}

func dialRuntimeClient(t *testing.T, client *RuntimeClient) *FrameToFrameSocket {
	t.Helper()
	socket, err := client.Execute(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	return socket
}

type timeoutCallImpl struct{ result chan error }

func (h *timeoutCallImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() {
		callCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := out.Call(callCtx, "slow-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "slow-1", Method: "slow"}}})
		h.result <- err
	}()
	return nil
}

// A server Call that times out reports context.DeadlineExceeded and tells
// the client with the schema's @ws_cancel frame.
func TestRuntimeServerCallTimeoutSendsCancel(t *testing.T) {
	impl := &timeoutCallImpl{result: make(chan error, 1)}
	socket := dialRuntime(t, impl)
	if err := socket.Send(context.Background(), &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send run: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := socket.Receive(ctx)
	if err != nil || first.GetHostCall() == nil {
		t.Fatalf("want host_call, got %+v (%v)", first, err)
	}
	second, err := socket.Receive(ctx)
	if err != nil {
		t.Fatalf("receive cancel: %v", err)
	}
	if c := second.GetCancel(); c == nil || c.Id != "slow-1" {
		t.Fatalf("want cancel for slow-1, got %+v", second)
	}
	if err := <-impl.result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
}

type recordingImpl struct{ frames chan *Frame }

func (h *recordingImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	h.frames <- req
	return nil
}

// A client Call whose ctx is cancelled reports context.Canceled and sends the
// @ws_cancel frame, which reaches the server handler rather than being taken
// for a reply.
func TestRuntimeClientCallCancelNotifiesServer(t *testing.T) {
	impl := &recordingImpl{frames: make(chan *Frame, 8)}
	socket := dialRuntime(t, impl)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, err := socket.Call(ctx, "c-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "c-1", Method: "work"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	var got []*Frame
	for len(got) < 2 {
		select {
		case frame := <-impl.frames:
			got = append(got, frame)
		case <-time.After(5 * time.Second):
			t.Fatalf("server saw %d of 2 frames", len(got))
		}
	}
	if got[0].GetHostCall() == nil {
		t.Fatalf("want host_call first, got %+v", got[0])
	}
	if c := got[1].GetCancel(); c == nil || c.Id != "c-1" {
		t.Fatalf("want cancel for c-1, got %+v", got[1])
	}
}

// Frames above the server's limit close the connection with 1009; an
// in-flight client Call fails with net.ErrClosed and exposes the close code.
func TestRuntimeMaxFrameBytes(t *testing.T) {
	impl := &recordingImpl{frames: make(chan *Frame, 8)}
	socket := dialRuntime(t, impl, WithMaxWSFrameBytes(1024))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	big := &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "big-1", Method: strings.Repeat("x", 4096)}}}
	_, err := socket.Call(ctx, "big-1", big)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("want net.ErrClosed, got %v", err)
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.StatusMessageTooBig {
		t.Fatalf("want close status 1009, got %v", err)
	}
}

type collidingCallImpl struct {
	frames chan *Frame
	result chan error
}

func (h *collidingCallImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		h.frames <- req
		return nil
	}
	go func() {
		callCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := out.Call(callCtx, "dup-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "dup-1", Method: "server"}}})
		if err == nil && reply.GetHostResult() == nil {
			err = errors.New("reply was not a host_result")
		}
		h.result <- err
	}()
	return nil
}

// A peer's own call that reuses the id of a call in flight the other way is
// the same variant that call sent, so it reaches the handler instead of being
// taken for the reply; the real reply (a different variant) still resolves it.
func TestRuntimeCollidingInboundCallIsNotAReply(t *testing.T) {
	impl := &collidingCallImpl{frames: make(chan *Frame, 8), result: make(chan error, 1)}
	socket := dialRuntime(t, impl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send run: %v", err)
	}
	if first, err := socket.Receive(ctx); err != nil || first.GetHostCall() == nil {
		t.Fatalf("want the server's host_call, got %+v (%v)", first, err)
	}
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "dup-1", Method: "client"}}}); err != nil {
		t.Fatalf("send colliding call: %v", err)
	}
	select {
	case frame := <-impl.frames:
		if c := frame.GetHostCall(); c == nil || c.Method != "client" {
			t.Fatalf("want the client's host_call at the handler, got %+v", frame)
		}
	case err := <-impl.result:
		t.Fatalf("the colliding call was taken for the reply: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the colliding call never reached the handler")
	}
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadHostResult{HostResult: &HostResult{Id: "dup-1", Value: "ok"}}}); err != nil {
		t.Fatalf("send reply: %v", err)
	}
	if err := <-impl.result; err != nil {
		t.Fatalf("server Call: %v", err)
	}
}

type failingImpl struct{}

func (failingImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() != nil {
		return errors.New("boom")
	}
	return nil
}

// A handler error closes with 1011 and the message as the reason - no
// off-schema frame is sent first.
func TestRuntimeHandlerErrorClosesWith1011(t *testing.T) {
	socket := dialRuntime(t, failingImpl{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	frame, err := socket.Receive(ctx)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.StatusInternalError || closeErr.Reason != "boom" {
		t.Fatalf("want close 1011 \"boom\", got frame %+v, err %v", frame, err)
	}
}

// A frame that does not decode closes with 1007, again with no frame first.
func TestRuntimeInvalidFrameClosesWith1007(t *testing.T) {
	url := serveRuntime(t, &recordingImpl{frames: make(chan *Frame, 8)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/v1/execute", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("not json")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusInvalidFramePayloadData {
		t.Fatalf("want close 1007, got data %q, err %v", data, err)
	}
}

type bigFrameImpl struct{}

func (bigFrameImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	return out.Send(ctx, &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "big", Method: strings.Repeat("x", 4096)}}})
}

// Hitting the client's own limit surfaces as the 1009 CloseError the peer
// sees, not a bare read error.
func TestRuntimeClientFrameLimitIsACloseError(t *testing.T) {
	client := NewRuntimeClient(serveRuntime(t, bigFrameImpl{}))
	client.MaxWSFrameBytes = 1024
	socket := dialRuntimeClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, err := socket.Receive(ctx)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.StatusMessageTooBig {
		t.Fatalf("want close 1009, got %v", err)
	}
}

type contextImpl struct{ done chan error }

func (h *contextImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() {
		<-out.Context().Done()
		h.done <- context.Cause(out.Context())
	}()
	return nil
}

func TestRuntimeOutContextEndsWhenClientCloses(t *testing.T) {
	impl := &contextImpl{done: make(chan error, 1)}
	socket := dialRuntime(t, impl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = socket.Close()
	select {
	case cause := <-impl.done:
		if websocket.CloseStatus(cause) != websocket.StatusNormalClosure {
			t.Fatalf("want the client's normal closure as the cause, got %v", cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("out.Context() never ended after the client closed")
	}
}

func TestRuntimeKeepAliveDropsUnresponsivePeer(t *testing.T) {
	impl := &contextImpl{done: make(chan error, 1)}
	url := serveRuntime(t, impl, WithWSPingInterval(100*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/v1/execute", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("{\"payload\":{\"type\":\"run\",\"run\":{\"code\":\"x\"}}}")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-impl.done:
	case <-time.After(3 * time.Second):
		t.Fatal("a peer that never answers pings was never dropped")
	}
}

func TestRuntimeClientFailsOnUndecodableFrame(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/execute", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("not json"))
		_, _, _ = conn.Read(r.Context())
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	socket := dialRuntimeClient(t, NewRuntimeClient(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := socket.Call(ctx, "c-1", &Frame{Payload: &FramePayloadHostCall{HostCall: &HostCall{Id: "c-1", Method: "x"}}})
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "decode frame") {
		t.Fatalf("want the decode error, got %v", err)
	}
}

type closingImpl struct{}

func (closingImpl) Execute(ctx context.Context, req *Frame, out *RuntimeExecuteOut) error {
	if req.GetRun() == nil {
		return nil
	}
	go func() { _ = out.Close(4000, "idle") }()
	return nil
}

func TestRuntimeHandlerClosesItsConnection(t *testing.T) {
	socket := dialRuntime(t, closingImpl{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := socket.Send(ctx, &Frame{Payload: &FramePayloadRun{Run: &RunRequest{Code: "x"}}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, err := socket.Receive(ctx)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != 4000 || closeErr.Reason != "idle" {
		t.Fatalf("want close 4000 \"idle\", got %v", err)
	}
}
`

// TestGeneratedWSCorrelatedRuntimeRoutesMultipleVariants actually runs a
// generated correlated @ws client and server against each other and
// verifies the full round trip, instead of only checking that the code
// compiles or contains expected substrings - see the harness doc comment.
func TestGeneratedWSCorrelatedRuntimeRoutesMultipleVariants(t *testing.T) {
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
		"go.mod":          "module " + wsCorrelatedRuntimeHarnessPkg + "\n\ngo 1.24\n\nrequire github.com/coder/websocket v1.8.15\n",
		"server.go":       string(server),
		"client.go":       string(client),
		"types.gen.go":    string(types),
		"validate.gen.go": string(validation),
		"harness_test.go": wsCorrelatedRuntimeHarness,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	run := exec.Command("go", "test", "-run", "TestRuntime", "-v", "-timeout", "120s", ".")
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime harness failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"--- PASS: TestRuntimeHarness",
		"--- PASS: TestRuntimeLateCallAfterClose",
		"--- PASS: TestRuntimeServerCallTimeoutSendsCancel",
		"--- PASS: TestRuntimeClientCallCancelNotifiesServer",
		"--- PASS: TestRuntimeMaxFrameBytes",
		"--- PASS: TestRuntimeCollidingInboundCallIsNotAReply",
		"--- PASS: TestRuntimeHandlerErrorClosesWith1011",
		"--- PASS: TestRuntimeInvalidFrameClosesWith1007",
		"--- PASS: TestRuntimeClientFrameLimitIsACloseError",
		"--- PASS: TestRuntimeOutContextEndsWhenClientCloses",
		"--- PASS: TestRuntimeKeepAliveDropsUnresponsivePeer",
		"--- PASS: TestRuntimeClientFailsOnUndecodableFrame",
		"--- PASS: TestRuntimeHandlerClosesItsConnection",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("expected %q in harness output:\n%s", want, out)
		}
	}
}
