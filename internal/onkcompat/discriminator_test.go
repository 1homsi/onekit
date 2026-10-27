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
	findings := compareSchemas(t, `namespace app;

model A {}

union MV {
  a: A,
}

model M {
  v: MV;
}
`, `namespace app;

model A {}

@Onkt.discriminator("type")
union MV {
  a: A,
}

model M {
  v: MV;
}
`)
	if len(findings) != 0 {
		t.Fatalf("explicit default discriminator reported as breaking: %+v", findings)
	}
	if findings := compareSchemas(t, `namespace app;

model A {}

union MV {
  a: A,
}

model M {
  v: MV;
}
`, `namespace app;

model A {}

@Onkt.discriminator("kind")
union MV {
  a: A,
}

model M {
  v: MV;
}
`); len(findings) == 0 {
		t.Fatal("changed discriminator not reported")
	}
}
