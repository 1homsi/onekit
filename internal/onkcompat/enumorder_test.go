package onkcompat

import (
	"strings"
	"testing"
)

func TestCompareFlagsReorderedNumberEncodedEnums(t *testing.T) {
	before := `namespace app;

enum Level {
  LOW,
  HIGH,
}

model M {
  @Onkt.encode("number")
  level: Level;
}
`
	after := `namespace app;

enum Level {
  UNSET,
  LOW,
  HIGH,
}

model M {
  @Onkt.encode("number")
  level: Level;
}
`
	findings := compareSchemas(t, before, after)
	if len(findings) != 2 || !strings.Contains(findings[1].Message, "moved from 0 to 1") {
		t.Fatalf("want position findings, got %+v", findings)
	}
	stringEncoded := compareSchemas(t, `namespace app;

enum Level {
  LOW,
  HIGH,
}

model M {
  level: Level;
}
`, `namespace app;

enum Level {
  UNSET,
  LOW,
  HIGH,
}

model M {
  level: Level;
}
`)
	if len(stringEncoded) != 0 {
		t.Fatalf("string-encoded enum reorder reported: %+v", stringEncoded)
	}
}
