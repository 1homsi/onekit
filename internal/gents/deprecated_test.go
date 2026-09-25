package gents

import (
	"strings"
	"testing"
)

func TestTSMarksDeprecatedFieldsAndMethods(t *testing.T) {
	file := compileTSSchema(t, `package app
message Note {
  /// Legacy title.
  title: string @deprecated("use heading")
  body: string @deprecated
}
service Notes {
  get(Note) -> Note @get("/notes") @deprecated("use fetch")
}
`)
	types, client := string(GenerateTypes(file)), string(GenerateClient(file))
	for _, check := range []struct{ out, want string }{
		{types, "/**\n * Legacy title.\n * @deprecated use heading\n */\ntitle"},
		{types, "/** @deprecated */\nbody"},
		{client, "/** @deprecated use fetch */\nasync get(req"},
	} {
		if !strings.Contains(check.out, check.want) {
			t.Fatalf("missing %q in:\n%s", check.want, check.out)
		}
	}
}
