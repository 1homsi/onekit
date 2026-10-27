package onkcompile

import (
	"strings"
	"testing"
)

func TestCompileRejectsFlattenKeyCollisions(t *testing.T) {
	for schema, want := range map[string]string{
		`
model Addr {
  city: string;
}

model M {
  city: string;
  @Onkt.flatten
  home: Addr;
}
`: `JSON key "city" is produced by both M.city and M.home.city`,
		`
model Addr {
  city: string;
}

model M {
  @Onkt.flatten
  a: Addr;
  @Onkt.flatten
  b: Addr;
}
`: `JSON key "city" is produced by both M.a.city and M.b.city`,
	} {
		_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, schema)}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	if _, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, `
model Addr {
  city: string;
}

model M {
  city: string;
  @Onkt.flatten("home_")
  home: Addr;
}
`)}}); err != nil {
		t.Fatalf("prefixed flatten rejected: %v", err)
	}
}
