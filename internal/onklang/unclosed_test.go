package onklang

import (
	"strings"
	"testing"
)

func TestParseReportsUnclosedBraceWithOpeningPosition(t *testing.T) {
	for src, want := range map[string]string{
		"message R {\n  id: string\n\nmessage Z { a: string }\n": "missing } for message R opened at 1:11",
		"enum E {\n  A\n":                     "missing } for enum E opened at 1:8",
		"service S {\n  base_path: \"/v1\"\n": "missing } for service S opened at 1:11",
	} {
		_, err := Parse(src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: want %q, got %v", src, want, err)
		}
	}
}
