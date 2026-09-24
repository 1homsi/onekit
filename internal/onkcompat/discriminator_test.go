package onkcompat

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
)

func compareSchemas(t *testing.T, before, after string) []Finding {
	t.Helper()
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*compile(t, before)})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*compile(t, after)})
	if err != nil {
		t.Fatal(err)
	}
	return Compare(oldPkg, newPkg)
}

func TestCompareTreatsExplicitDefaultDiscriminatorAsUnchanged(t *testing.T) {
	findings := compareSchemas(t, `package app
message A {}
message M { v: oneof { a: A } }`, `package app
message A {}
message M { v: oneof(discriminator: "type") { a: A } }`)
	if len(findings) != 0 {
		t.Fatalf("explicit default discriminator reported as breaking: %+v", findings)
	}
	if findings := compareSchemas(t, `package app
message A {}
message M { v: oneof { a: A } }`, `package app
message A {}
message M { v: oneof(discriminator: "kind") { a: A } }`); len(findings) == 0 {
		t.Fatal("changed discriminator not reported")
	}
}
