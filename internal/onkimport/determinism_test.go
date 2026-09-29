package onkimport

import (
	"strings"
	"testing"
)

// mapKeys feeds an unsorted Go map range into a user-visible warning message,
// so the warning text varies between runs on identical input.
const unsupportedCompositionSpec = `
openapi: 3.0.3
info:
  title: t
  version: 1.0.0
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                unevaluatedProperties: false
                properties:
                  ddd: { type: string }
                  eee: { type: string }
                  fff: { type: string }
                  zzz: { type: string }
                  aaa: { type: string }
                  mmm: { type: string }
                  bbb: { type: string }
                  ccc: { type: string }
`

// The warning text must be stable across repeated imports of the same spec.
func TestImportWarningsAreDeterministic(t *testing.T) {
	first, err := Import([]byte(unsupportedCompositionSpec), Options{Package: "demo"})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	want := strings.Join(first.Warnings, "\n")
	if want == "" {
		t.Skip("fixture produced no warnings")
	}

	for range 25 {
		again, err := Import([]byte(unsupportedCompositionSpec), Options{Package: "demo"})
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if got := strings.Join(again.Warnings, "\n"); got != want {
			t.Fatalf("warning text is not deterministic:\nfirst: %q\ngot:   %q", want, got)
		}
	}
}

// mapKeys must itself return a sorted slice.
func TestMapKeysIsSorted(t *testing.T) {
	m := map[string]any{"ccc": nil, "aaa": nil, "bbb": nil}
	got := mapKeys(m)

	want := []string{"aaa", "bbb", "ccc"}
	if len(got) != len(want) {
		t.Fatalf("mapKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mapKeys = %v, want %v (sorted)", got, want)
		}
	}
}
