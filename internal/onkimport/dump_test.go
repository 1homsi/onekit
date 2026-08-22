package onkimport

import (
	"os"
	"testing"
)

func TestDumpFixture(t *testing.T) {
	r, err := Import([]byte(petstoreMini), Options{})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("/tmp/imported.onk", r.Source, 0o644)
}
