package onek

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPTools(t *testing.T) {
	root, source := languageFixture(t)
	server, err := NewLanguageMCPServer(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 5 {
		t.Fatalf("tools: %+v", listed.Tools)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("missing read-only hint")
		}
	}
	call := func(name string, args any) *mcp.CallToolResult {
		t.Helper()
		result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := call("onekit_project", map[string]any{})
	if result.IsError {
		t.Fatalf("project error: %+v", result.Content[0])
	}
	var project LanguageSnapshot
	data, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if len(project.Diagnostics) != 0 || len(project.Files) != 3 {
		t.Fatalf("project: %+v", project)
	}
	pos := positionOf(t, source, "common.User")
	args := map[string]any{"path": "schema/api/service.onk", "line": pos.Line, "character": pos.Character}
	for _, name := range []string{"onekit_hover", "onekit_definition", "onekit_references"} {
		result = call(name, args)
		if result.IsError {
			t.Fatalf("%s: %+v", name, result)
		}
		data, _ = json.Marshal(result.StructuredContent)
		var out positionOutput
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		if out.Symbol == nil || out.Symbol.QualifiedName != "common.User" {
			t.Fatalf("%s: %+v", name, out)
		}
		want := 1
		if name == "onekit_references" {
			want = 3
		}
		if len(out.Locations) != want {
			t.Fatalf("%s locations: %+v", name, out.Locations)
		}
	}
	result = call("onekit_symbols", map[string]any{"query": "common.User"})
	if result.IsError {
		t.Fatal("symbol query failed")
	}
	for _, args := range []map[string]any{{"project": ".."}, {"project": root}} {
		if !call("onekit_project", args).IsError {
			t.Fatalf("accepted outside project: %+v", args)
		}
	}
	if !call("onekit_hover", map[string]any{"path": "../secret.onk", "line": 0, "character": 0}).IsError {
		t.Fatal("accepted traversal")
	}
	// Schema validation, compiler errors, and fresh disk reads are observable to clients.
	invalid, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "onekit_hover", Arguments: map[string]any{"path": 1}})
	if err == nil && !invalid.IsError {
		t.Fatal("invalid arguments accepted")
	}
	writeTestFile(t, filepath.Join(root, "schema/api/service.onk"), "message Broken { field: Missing }")
	result = call("onekit_project", map[string]any{})
	data, _ = json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if len(project.Diagnostics) != 1 {
		t.Fatalf("stale validation: %+v", project)
	}
}
