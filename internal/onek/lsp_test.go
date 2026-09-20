package onek

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
