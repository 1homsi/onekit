package onek

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLSPRenamesTypesAcrossFiles(t *testing.T) {
	root, source := languageFixture(t)
	path := filepath.Join(root, "schema/api/service.onk")
	server := &languageServer{root: root, overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}}
	call := func(method string, params map[string]any) (any, *rpcError) {
		params["textDocument"] = map[string]any{"uri": fileURI(path)}
		raw, _ := json.Marshal(params)
		return server.handle(rpcRequest{Method: method, Params: raw})
	}
	at := positionOf(t, source, "common.User")
	at.Character += len("common.")

	prepared, callErr := call("textDocument/prepareRename", map[string]any{"position": at})
	if callErr != nil {
		t.Fatal(callErr.Message)
	}
	data, _ := json.Marshal(prepared)
	if !strings.Contains(string(data), `"placeholder":"User"`) || !strings.Contains(string(data), `"start":{"line":5,"character":27}`) {
		t.Fatalf("prepareRename = %s", data)
	}

	result, callErr := call("textDocument/rename", map[string]any{"position": at, "newName": "Member"})
	if callErr != nil {
		t.Fatal(callErr.Message)
	}
	data, _ = json.Marshal(result)
	var edit struct {
		Changes map[string][]struct {
			Range   Range  `json:"range"`
			NewText string `json:"newText"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(data, &edit); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for uri, edits := range edit.Changes {
		for _, e := range edits {
			lines = append(lines, filepath.Base(filepath.Dir(uri))+":"+string(rune('0'+e.Range.Start.Line))+":"+e.NewText)
		}
	}
	sort.Strings(lines)
	want := "api:4:Member api:5:Member api:7:Member common:2:Member"
	if strings.Join(lines, " ") != want {
		t.Fatalf("edits = %v, want %s", lines, want)
	}

	if _, callErr := call("textDocument/rename", map[string]any{"position": at, "newName": "State"}); callErr == nil {
		t.Fatal("rename onto an existing type was accepted")
	}
	if _, callErr := call("textDocument/rename", map[string]any{"position": at, "newName": "9bad"}); callErr == nil {
		t.Fatal("invalid identifier was accepted")
	}
}
