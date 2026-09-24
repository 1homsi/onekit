package onklang

import "testing"

func TestDocCommentsKeepIndentation(t *testing.T) {
	src := "/// Example:\n///     client.get(id)\n///\n/// Done.\nmessage M {\n}\n"
	file, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := file.Messages[0].Doc, "Example:\n    client.get(id)\n\nDone."; got != want {
		t.Fatalf("doc = %q, want %q", got, want)
	}
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("format changed doc layout:\n%s", out)
	}
}
