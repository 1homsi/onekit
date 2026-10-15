package onkcompile

import (
	"strings"
	"testing"
)

func TestOneofVariantsCannotCollideWithFieldsOrOtherVariants(t *testing.T) {
	cases := []struct {
		name, schema, want string
	}{
		{"variant named like the oneof", `message B { s: string }
message O { u: oneof(discriminator: "t") { u: B @tag("a") } }`, `oneof variant "u" collides with "u"`},
		{"variant named like a field", `message B { s: string }
message O { s: string  u: oneof(discriminator: "t") { s: B @tag("a") } }`, `oneof variant "s" collides with "s"`},
		{"variant named like a field after conversion", `message B { s: string }
message O { foo_bar: string  u: oneof(discriminator: "t") { fooBar: B @tag("a") } }`, `collides with "foo_bar"`},
		{"variant in two oneofs", `message B { s: string }
message O {
  a: oneof(discriminator: "ta") { x: B @tag("a") }
  b: oneof(discriminator: "tb") { x: B @tag("b") }
}`, `oneof variant "x" collides with "x"`},
		{"distinct names", `message B { s: string }
message O {
  s: string
  a: oneof(discriminator: "ta") { x: B @tag("a") }
  b: oneof(discriminator: "tb") { y: B @tag("b") }
}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.onk", AST: parseOrFatal(t, "package app\n"+tc.schema+"\n")}}, CompileOptions{})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
