package gengo

import (
	"bytes"
	"strings"
	"testing"
)

func formatOf(t *testing.T, src string) []byte {
	t.Helper()
	p := newPrinter(nil)
	p.P(src)
	out, err := p.Format()
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	return out
}

func TestFormatMemoReturnsIdenticalOutput(t *testing.T) {
	src := "package x\nfunc  f( ) {\nreturn\n}"
	first := formatOf(t, src)
	second := formatOf(t, src)
	if !bytes.Equal(first, second) {
		t.Fatalf("memoized output differs:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(string(first), "func f() {") {
		t.Fatalf("output not gofmt-formatted: %s", first)
	}
}

func TestFormatMemoIsolatesCallersFromMutation(t *testing.T) {
	src := "package isolated\nvar  a  =  1"
	first := formatOf(t, src)
	want := string(first)
	for i := range first {
		first[i] = 'X'
	}
	if got := string(formatOf(t, src)); got != want {
		t.Fatalf("mutating a returned slice leaked into the memo: %q != %q", got, want)
	}
}

func TestFormatMemoDoesNotCacheErrors(t *testing.T) {
	for i := range 2 {
		p := newPrinter(nil)
		p.P("package broken\nfunc (")
		if _, err := p.Format(); err == nil {
			t.Fatalf("attempt %d: expected a format error for invalid source", i)
		}
	}
}

func TestFormatMemoStaysWithinLimit(t *testing.T) {
	block := "package bulk\nvar v = \"" + strings.Repeat("a", 1<<20) + "\"\n"
	for i := range 80 {
		formatOf(t, block+strings.Repeat(" ", i)+"var w"+strings.Repeat("z", i)+" = 1")
	}
	formatMemo.Lock()
	size := formatMemo.size
	formatMemo.Unlock()
	if size > formatMemoLimit {
		t.Fatalf("memo holds %d bytes, limit is %d", size, formatMemoLimit)
	}
}
