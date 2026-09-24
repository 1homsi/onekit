package onek

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLSPFormatsDocuments(t *testing.T) {
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "onekit.toml"), "module = \"example.com/test\"\n")
	path := filepath.Join(root, "api.onk")
	writeTestFile(t, path, "message M{a:string}\n")
	server := &languageServer{root: root, overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}}
	params, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}})
	result, callErr := server.handle(rpcRequest{Method: "textDocument/formatting", Params: params})
	if callErr != nil {
		t.Fatal(callErr.Message)
	}
	data, _ := json.Marshal(result)
	want := `[{"newText":"message M {\n  a: string\n}\n","range":{"end":{"character":0,"line":1},"start":{"character":0,"line":0}}}]`
	if string(data) != want {
		t.Fatalf("edits = %s\nwant %s", data, want)
	}
}
