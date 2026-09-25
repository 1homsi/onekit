package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsFlattenKeyCollisions(t *testing.T) {
	for schema, want := range map[string]string{
		`message Addr { city: string }
message M { city: string  home: Addr @flatten }`: `JSON key "city" is produced by both M.city and M.home.city`,
		`message Addr { city: string }
message M { a: Addr @flatten  b: Addr @flatten }`: `JSON key "city" is produced by both M.a.city and M.b.city`,
	} {
		_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	if _, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, `
message Addr { city: string }
message M { city: string  home: Addr @flatten(prefix: "home_") }`)}}); err != nil {
		t.Fatalf("prefixed flatten rejected: %v", err)
	}
}
