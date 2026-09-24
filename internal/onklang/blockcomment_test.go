package onklang

import "testing"

func TestFormatKeepsIndentedBlockCommentsStable(t *testing.T) {
	src := `message M {
    /*
     * about a
     *   indented detail
     */
    a: string
}
`
	once, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(string(once))
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Fatalf("format is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	want := "message M {\n  /*\n   * about a\n   *   indented detail\n   */\n  a: string\n}\n"
	if string(once) != want {
		t.Fatalf("unexpected layout:\n%s", once)
	}
}
