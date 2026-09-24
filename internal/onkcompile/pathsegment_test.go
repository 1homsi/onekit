package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresWholeSegmentPathParameters(t *testing.T) {
	for _, route := range []string{"/users/{id}.json", "/a{id}", "/{id}-{name}"} {
		t.Run(route, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R { id: string  name: string }
service API { a(R) -> R @get("`+route+`") }
`)}})
			if err == nil || !strings.Contains(err.Error(), "whole segment") {
				t.Fatalf("want whole segment error, got %v", err)
			}
		})
	}
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message R { id: string }
service API { a(R) -> R @get("/users/{id}") }
`)}}); err != nil {
		t.Fatalf("whole segment parameter rejected: %v", err)
	}
}
