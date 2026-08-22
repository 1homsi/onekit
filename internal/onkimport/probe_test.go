package onkimport

import (
	"testing"
)

func TestProbeNaming(t *testing.T) {
	got := capitalize(pascalIdent("addPet"))
	if got != "AddPet" {
		t.Fatalf("naming pipeline broken: %q", got)
	}
}
