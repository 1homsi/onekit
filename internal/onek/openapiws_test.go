package onek

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOpenAPIServiceDocumentResolvesWebSocketFrameRefs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"x\"\n\n[generate.openapi]\nout = \"./docs\"\n")
	writeTestFile(t, filepath.Join(dir, "api.onk"), `
message ChatIn { text: string }
message ChatOut { text: string  author: string }
service Chat { chat(ChatIn) -> ChatOut @ws("/chat") }
`)
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, ref := range regexp.MustCompile(`#/components/schemas/([A-Za-z0-9_.]+)`).FindAllStringSubmatch(doc, -1) {
		if !strings.Contains(doc, "\n        "+ref[1]+":") && !strings.Contains(doc, "\n    "+ref[1]+":") {
			t.Fatalf("dangling $ref %s in:\n%s", ref[0], doc)
		}
	}
	if !strings.Contains(doc, "ChatIn:") || !strings.Contains(doc, "ChatOut:") {
		t.Fatalf("frame schemas missing:\n%s", doc)
	}
}
