package onek

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCheckSourceAndFormat(t *testing.T) {
	root, _ := languageFixture(t)
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
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s failed: %v %+v", name, err, result)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
	broken := call("onekit_check_source", map[string]any{"path": "schema/api/draft.onk", "source": "message Draft { field: Missing }"})
	if diags, _ := broken["diagnostics"].([]any); len(diags) != 1 {
		t.Fatalf("want one diagnostic for the proposed source, got %v", broken)
	}
	clean := call("onekit_check_source", map[string]any{"path": "schema/api/draft.onk", "source": "message Draft { field: string }"})
	if diags, _ := clean["diagnostics"].([]any); len(diags) != 0 {
		t.Fatalf("clean source reported diagnostics: %v", clean)
	}
	formatted := call("onekit_format", map[string]any{"source": "message M{a:string}\n"})
	if formatted["formatted"] != "message M {\n  a: string\n}\n" || formatted["changed"] != true {
		t.Fatalf("format: %v", formatted)
	}
}
