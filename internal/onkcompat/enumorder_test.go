package onkcompat

import (
	"strings"
	"testing"
)

func TestCompareFlagsReorderedNumberEncodedEnums(t *testing.T) {
	before := `package app
enum Level { LOW  HIGH }
message M { level: Level @encode(number) }`
	after := `package app
enum Level { UNSET  LOW  HIGH }
message M { level: Level @encode(number) }`
	findings := compareSchemas(t, before, after)
	if len(findings) != 2 || !strings.Contains(findings[1].Message, "moved from 0 to 1") {
		t.Fatalf("want position findings, got %+v", findings)
	}
	stringEncoded := compareSchemas(t, `package app
enum Level { LOW  HIGH }
message M { level: Level }`, `package app
enum Level { UNSET  LOW  HIGH }
message M { level: Level }`)
	if len(stringEncoded) != 0 {
		t.Fatalf("string-encoded enum reorder reported: %+v", stringEncoded)
	}
}
