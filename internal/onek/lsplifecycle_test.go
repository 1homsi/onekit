package onek

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestLSPIgnoresUnknownNotificationsAndReportsEarlyExit(t *testing.T) {
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "onekit.toml"), "module = \"example.com/test\"\n")
	writeTestFile(t, filepath.Join(root, "api.onk"), "message M {}\n")
	session := func(shutdown bool) (string, error) {
		var input, output bytes.Buffer
		for _, m := range []map[string]any{
			{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"rootUri": fileURI(root)}},
			{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}},
			{"jsonrpc": "2.0", "method": "$/setTrace", "params": map[string]any{"value": "off"}},
			{"jsonrpc": "2.0", "method": "workspace/didChangeConfiguration", "params": map[string]any{}},
		} {
			if err := writeLSPMessage(&input, m); err != nil {
				t.Fatal(err)
			}
		}
		if shutdown {
			_ = writeLSPMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
		}
		_ = writeLSPMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
		err := RunLSP(&input, &output, "")
		return output.String(), err
	}
	out, err := session(true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "method not found") {
		t.Fatalf("unknown notifications were reported as errors:\n%s", out)
	}
	if _, err := session(false); !errors.Is(err, errExitBeforeShutdown) {
		t.Fatalf("exit before shutdown: want errExitBeforeShutdown, got %v", err)
	}
}
