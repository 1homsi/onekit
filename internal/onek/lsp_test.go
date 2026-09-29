package onek

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestLSPWorkspaceAndUnsavedEdits(t *testing.T) {
	root, source := languageFixture(t)
	path := filepath.Join(root, "schema/api/service.onk")
	uri := fileURI(path)
	var input, output bytes.Buffer
	request := func(id any, method string, params any) {
		t.Helper()
		m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			m["id"] = id
		}
		if err := writeLSPMessage(&input, m); err != nil {
			t.Fatal(err)
		}
	}
	request(1, "initialize", map[string]any{"rootUri": fileURI(root)})
	request(nil, "initialized", map[string]any{})
	request(nil, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}})
	pos := positionOf(t, source, "common.User")
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": pos, "context": map[string]bool{"includeDeclaration": true}}
	request(2, "textDocument/definition", params)
	request(3, "textDocument/references", params)
	request(4, "textDocument/hover", params)
	request(5, "textDocument/documentSymbol", params)
	request(6, "workspace/symbol", map[string]string{"query": "common.User"})
	bad := strings.Replace(source, "user: User", "user: Missing", 1)
	change := func(text string, version int) {
		request(nil, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]string{"text": text}}})
	}
	change(bad, 2)
	request(7, "textDocument/definition", params)
	change(source, 1) // Stale editor update must not replace version 2.
	request(8, "textDocument/definition", params)
	request(nil, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}})
	request(9, "textDocument/definition", params)
	request(10, "shutdown", nil)
	request(nil, "exit", nil)
	if err := RunLSP(&input, &output, ""); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	responses := map[int]map[string]any{}
	sawError, cleared := false, false
	for reader.Buffered() > 0 || output.Len() > 0 {
		data, err := readLSPMessage(reader)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if id, ok := m["id"].(float64); ok {
			responses[int(id)] = m
			continue
		}
		if m["method"] == "textDocument/publishDiagnostics" {
			p := m["params"].(map[string]any)
			if p["uri"] != uri {
				continue
			}
			diagnostics := p["diagnostics"].([]any)
			if len(diagnostics) > 0 {
				sawError = true
			} else if sawError {
				cleared = true
			}
		}
	}
	if len(responses) != 10 {
		t.Fatalf("responses: %+v", responses)
	}
	for id, m := range responses {
		if m["error"] != nil {
			t.Fatalf("response %d: %+v", id, m)
		}
	}
	definition := responses[2]["result"].(map[string]any)
	if definition["uri"] != fileURI(filepath.Join(root, "schema/common/models.onk")) {
		t.Fatalf("definition: %+v", definition)
	}
	if len(responses[3]["result"].([]any)) != 4 {
		t.Fatalf("refs: %+v", responses[3])
	}
	if !strings.Contains(fmt.Sprint(responses[4]), "A user shared") {
		t.Fatalf("hover: %+v", responses[4])
	}
	if len(responses[5]["result"].([]any)) == 0 || len(responses[6]["result"].([]any)) != 2 {
		t.Fatal("missing symbols")
	}
	if responses[7]["result"] != nil || responses[8]["result"] != nil || responses[9]["result"] == nil {
		t.Fatal("incorrect overlay lifecycle")
	}
	if !sawError || !cleared {
		t.Fatalf("diagnostics error=%v cleared=%v", sawError, cleared)
	}
}
func TestLSPFramingAndURI(t *testing.T) {
	for _, input := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 999999999\r\n\r\n", "Content-Length: 1\r\nContent-Length: 1\r\n\r\nx", "X: y\r\n\r\n", strings.Repeat("x", 9000)} {
		if _, err := readLSPMessage(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatal("accepted invalid header")
		}
	}
	for _, uri := range []string{"https://example.com/a.onk", "file://remote/a.onk", "file:relative.onk"} {
		if _, err := pathFromURI(uri); err == nil {
			t.Fatalf("accepted %s", uri)
		}
	}
	path := filepath.Join(t.TempDir(), "space name.onk")
	got, err := pathFromURI(fileURI(path))
	if err != nil || got != path {
		t.Fatalf("URI round trip: %s %v", got, err)
	}
}

func TestLSPRejectsOutsideSchemaWithoutPoisoningWorkspace(t *testing.T) {
	root, _ := languageFixture(t)
	var output bytes.Buffer
	server := &languageServer{root: root, overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}, out: &output}
	params, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": fileURI(filepath.Join(root, "outside.onk")), "text": "message Outside {}", "version": 1}})
	if _, err := server.handle(rpcRequest{Method: "textDocument/didOpen", Params: params}); err == nil {
		t.Fatal("accepted file outside schema_root")
	}
	if len(server.overlays) != 0 {
		t.Fatal("rejected document poisoned overlays")
	}
	params, _ = json.Marshal(map[string]string{"query": "User"})
	if _, err := server.handle(rpcRequest{Method: "workspace/symbol", Params: params}); err != nil {
		t.Fatal(err)
	}
}

func TestLSPDecoratorHoverAndCompletion(t *testing.T) {
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "onekit.toml"), "module = \"example.com/test\"\n")
	source := "package rt\nmessage Call {\n id: string @ws_id\n timeout_ms: int64 @ws_timeout\n}\nmessage Frame { payload: oneof(discriminator: \"type\") { call: Call } }\nservice Runtime { execute(Frame) -> Frame @ws(\"/x\") }\n"
	path := filepath.Join(root, "rt.onk")
	writeTestFile(t, path, source)
	uri := fileURI(path)
	var input, output bytes.Buffer
	request := func(id any, method string, params any) {
		t.Helper()
		m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			m["id"] = id
		}
		if err := writeLSPMessage(&input, m); err != nil {
			t.Fatal(err)
		}
	}
	editing := strings.Replace(source, " id: string @ws_id", " id: string @ws_", 1)
	request(1, "initialize", map[string]any{"rootUri": fileURI(root)})
	request(2, "textDocument/hover", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": positionOf(t, source, "timeout_ms")})
	request(3, "textDocument/hover", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": positionOf(t, source, "execute")})
	request(nil, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "text": editing, "version": 1}})
	end := positionOf(t, editing, "@ws_\n")
	end.Character += len("@ws_")
	request(4, "textDocument/completion", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": end})
	request(5, "shutdown", nil)
	request(nil, "exit", nil)
	if err := RunLSP(&input, &output, ""); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	responses := map[int]string{}
	for reader.Buffered() > 0 || output.Len() > 0 {
		data, err := readLSPMessage(reader)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if id, ok := m["id"].(float64); ok {
			responses[int(id)] = string(data)
		}
	}
	for id, want := range map[int][]string{
		2: {"timeout_ms: int64 @ws_timeout", "remaining time in milliseconds"},
		3: {`@ws(\"/x\")`, "bidirectional WebSocket RPC"},
		4: {`"label":"@ws_cancel"`, `"label":"@ws_id"`, `"label":"@ws_timeout"`},
	} {
		for _, text := range want {
			if !strings.Contains(responses[id], text) {
				t.Fatalf("response %d missing %q: %s", id, text, responses[id])
			}
		}
	}
	if strings.Contains(responses[4], `"label":"@raw"`) {
		t.Fatalf("completion ignored the typed prefix: %s", responses[4])
	}
}

// runLSPSession drives the real RunLSP loop over in-process pipes, the way an
// editor does: requests stream in while responses and notifications stream out.
// A handler that panics takes the process (and this test binary) down with it,
// which is exactly the failure these tests are here to catch.
func runLSPSession(t *testing.T, script func(send func(id any, method string, params any))) map[int]map[string]any {
	t.Helper()
	in, requests := io.Pipe()
	out, delivered := io.Pipe()
	served := make(chan error, 1)
	go func() {
		err := RunLSP(in, delivered, "")
		_ = delivered.Close()
		served <- err
	}()
	messages := make(chan map[string]any, 64)
	go func() {
		defer close(messages)
		reader := bufio.NewReader(out)
		for {
			data, err := readLSPMessage(reader)
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			if _, ok := m["id"]; ok { // Drop publishDiagnostics and other notifications.
				messages <- m
			}
		}
	}()
	script(func(id any, method string, params any) {
		t.Helper()
		m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			m["id"] = id
		}
		if err := writeLSPMessage(requests, m); err != nil {
			t.Errorf("write %s: %v", method, err)
		}
	})
	_ = requests.Close()
	collected := map[int]map[string]any{}
	for m := range messages {
		id, ok := m["id"].(float64)
		if !ok {
			continue
		}
		collected[int(id)] = m
	}
	if err := <-served; err != nil {
		t.Fatalf("RunLSP: %v", err)
	}
	return collected
}

func TestLSPNegativePositionIsRejectedWithoutKillingTheServer(t *testing.T) {
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "onekit.toml"), "module = \"example.com/test\"\n")
	source := "package rt\nmessage Call {\n id: string @ws_id\n}\n"
	path := filepath.Join(root, "rt.onk")
	writeTestFile(t, path, source)
	uri := fileURI(path)
	editing := strings.Replace(source, "@ws_id", "@ws_", 1)
	at := positionOf(t, editing, "@ws_\n")
	at.Character += len("@ws_")

	// Line 2 is the decorated field, so a negative character used to index
	// before the start of the line and panic inside decoratorCompletion.
	negative := []Position{{2, -1}, {2, -2}, {2, -5}, {-1, 0}, {-1, -1}}
	responses := runLSPSession(t, func(send func(id any, method string, params any)) {
		send(1, "initialize", map[string]any{"rootUri": fileURI(root)})
		send(nil, "initialized", map[string]any{})
		send(nil, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "text": editing, "version": 1}})
		for i, position := range negative {
			send(i+2, "textDocument/completion", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position})
		}
		// Everything after the bad positions must still be served: a dead
		// server is the bug this test exists to prevent.
		send(100, "textDocument/completion", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": at})
		send(101, "textDocument/hover", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": positionOf(t, editing, "Call")})
		send(102, "shutdown", nil)
		send(nil, "exit", nil)
	})

	for i, position := range negative {
		response, ok := responses[i+2]
		if !ok {
			t.Fatalf("no response for position %+v: %+v", position, responses)
		}
		failure, ok := response["error"].(map[string]any)
		if !ok {
			t.Fatalf("position %+v: expected an error, got %+v", position, response)
		}
		if code, _ := failure["code"].(float64); code != -32602 {
			t.Fatalf("position %+v: error code %v in %+v", position, failure["code"], failure)
		}
	}
	if len(responses) != len(negative)+4 {
		t.Fatalf("server stopped answering: %+v", responses)
	}
	items, ok := responses[100]["result"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("completion after negative positions: %+v", responses[100])
	}
	if responses[101]["result"] == nil {
		t.Fatalf("hover after negative positions: %+v", responses[101])
	}
}

func TestDecoratorCompletionIgnoresNegativePosition(t *testing.T) {
	line := "message Call { id: string @ws_ }"
	var output bytes.Buffer
	server := &languageServer{overlays: map[string]string{"rt.onk": "package rt\n" + line + "\n"}, versions: map[string]int{}, published: map[string]bool{}, out: &output}
	for _, position := range []Position{{1, -1}, {1, -5}, {-1, 0}, {-1, -1}} {
		if items := server.decoratorCompletion("rt.onk", position); len(items) != 0 {
			t.Fatalf("position %+v: %+v", position, items)
		}
	}
	if items := server.decoratorCompletion("rt.onk", Position{1, strings.Index(line, "@ws_") + len("@ws_")}); len(items) == 0 {
		t.Fatal("no completions for a valid position")
	}
}

func TestLSPHandlerPanicBecomesInternalError(t *testing.T) {
	var output bytes.Buffer
	server := &languageServer{overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}, out: &output}
	ok := &rpcOutcome{}
	server.respond(ok, func() (any, *rpcError) { return "value", nil })
	if ok.result != "value" || ok.err != nil {
		t.Fatalf("passthrough: %+v", ok)
	}
	failed := &rpcOutcome{}
	server.respond(failed, func() (any, *rpcError) { return nil, &rpcError{-32602, "invalid params"} })
	if failed.result != nil || failed.err == nil || failed.err.Code != -32602 {
		t.Fatalf("passthrough of an error: %+v", failed)
	}
	panicked := &rpcOutcome{}
	server.respond(panicked, func() (any, *rpcError) { panic("handler bug") })
	if panicked.result != nil || panicked.err == nil || panicked.err.Code != -32603 {
		t.Fatalf("panic: %+v", panicked)
	}
	if !strings.Contains(output.String(), "handler bug") {
		t.Fatalf("panic was not reported to the client log: %s", output.String())
	}
}
