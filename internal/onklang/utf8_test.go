package onklang

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// lexString scans the literal body one byte at a time and hands decoding to
// strconv.Unquote. Go's unquoteChar decodes with utf8.DecodeRuneInString and
// treats a decoding error as a valid RuneError, so malformed bytes become
// U+FFFD with a nil error. Parse then succeeds and `onek fmt` writes the
// mangled bytes back to disk, silently and irreversibly.
func TestParseRejectsInvalidUTF8InStringLiteral(t *testing.T) {
	src := "package p\n\nmessage M {\n  a: string @format(\"caf\xff\")\n}\n"

	if _, err := Parse(src); err == nil {
		t.Fatalf("expected an error for invalid UTF-8 in a string literal, got none")
	}
}

// The same invalid byte inside a line comment is preserved verbatim, so the
// current behaviour is inconsistent, not a deliberate choice.
func TestInvalidUTF8InCommentIsPreserved(t *testing.T) {
	src := "package p\n\n// caf\xff\nmessage M {\n  a: string\n}\n"

	if _, err := Parse(src); err != nil {
		t.Fatalf("parse: %v", err)
	}
	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(string(formatted), "\xff") {
		t.Errorf("comment byte was not preserved:\n%q", formatted)
	}
}

// Truncated (multi-byte prefix) and lone-surrogate sequences must be rejected
// too, not just a single stray 0xFF.
func TestParseRejectsTruncatedAndSurrogateSequences(t *testing.T) {
	cases := map[string]string{
		"truncated 3-byte": "package p\n\nmessage M {\n  a: string @format(\"\xe6\x97\")\n}\n",
		"surrogate":        "package p\n\nmessage M {\n  a: string @format(\"a\xed\xa0\x80b\")\n}\n",
		"overlong":         "package p\n\nmessage M {\n  a: string @format(\"\xc0\xaf\")\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(src); err == nil {
				t.Errorf("expected an error, got none")
			}
		})
	}
}

// Well-formed multi-byte UTF-8 must keep working.
func TestParseAcceptsValidUTF8InStringLiteral(t *testing.T) {
	const jp = "日本語"
	src := "package p\n\nmessage M {\n  a: string @format(\"" + jp + "\")\n}\n"

	if _, err := Parse(src); err != nil {
		t.Fatalf("parse: %v", err)
	}
	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(string(formatted), jp) {
		t.Errorf("expected the literal to survive formatting, got %q", formatted)
	}
	if !utf8.Valid(formatted) {
		t.Error("formatted output is not valid UTF-8")
	}
}
