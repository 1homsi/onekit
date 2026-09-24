package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsFieldBoundAsPathAndQuery(t *testing.T) {
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R { id: string @query }
service API { get(R) -> R @get("/u/{id}") }
`)}})
	if err == nil || !strings.Contains(err.Error(), `request field "id" cannot be both a path and query binding`) {
		t.Fatalf("want path/query conflict, got %v", err)
	}
}
