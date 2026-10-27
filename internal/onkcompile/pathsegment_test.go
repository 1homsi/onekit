package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRequiresWholeSegmentPathParameters(t *testing.T) {
	for _, route := range []string{"/users/{id}.json", "/a{id}", "/{id}-{name}"} {
		t.Run(route, func(t *testing.T) {
			_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model R { id: string; name: string; }
interface API { @get @route("`+route+`") a(...R): R; }
`)}})
			if err == nil || !strings.Contains(err.Error(), "whole segment") {
				t.Fatalf("want whole segment error, got %v", err)
			}
		})
	}
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model R { id: string; }
interface API { @get @route("/users/{id}") a(...R): R; }
`)}}); err != nil {
		t.Fatalf("whole segment parameter rejected: %v", err)
	}
}
