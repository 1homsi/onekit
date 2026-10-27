package onkcompile

import (
	"strings"
	"testing"
)

func TestOneofVariantsCannotCollideWithFieldsOrOtherVariants(t *testing.T) {
	cases := []struct {
		name, schema, want string
	}{
		{"variant named like the oneof", `
model B {
  s: string;
}

@Onkt.discriminator("t")
union OU {
  @Onkt.tag("a") u: B,
}

model O {
  u: OU;
}
`, `oneof variant "u" collides with "u"`},
		{"variant named like a field", `
model B {
  s: string;
}

@Onkt.discriminator("t")
union OU {
  @Onkt.tag("a") s: B,
}

model O {
  s: string;
  u: OU;
}
`, `oneof variant "s" collides with "s"`},
		{"variant named like a field after conversion", `
model B {
  s: string;
}

@Onkt.discriminator("t")
union OU {
  @Onkt.tag("a") fooBar: B,
}

model O {
  foo_bar: string;
  u: OU;
}
`, `collides with "foo_bar"`},
		{"variant in two oneofs", `
model B {
  s: string;
}

@Onkt.discriminator("ta")
union OA {
  @Onkt.tag("a") x: B,
}

@Onkt.discriminator("tb")
union OB {
  @Onkt.tag("b") x: B,
}

model O {
  a: OA;
  b: OB;
}
`, `oneof variant "x" collides with "x"`},
		{"not generating Go", `
model B {
  s: string;
}

@Onkt.discriminator("t")
union OU {
  @Onkt.tag("a") u: B,
}

model O {
  u: OU;
}
`, ""},
		{"distinct names", `
model B {
  s: string;
}

@Onkt.discriminator("ta")
union OA {
  @Onkt.tag("a") x: B,
}

@Onkt.discriminator("tb")
union OB {
  @Onkt.tag("b") y: B,
}

model O {
  s: string;
  a: OA;
  b: OB;
}
`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			options := CompileOptions{}
			if tc.name == "not generating Go" {
				options.Targets = []string{"ts-client", "python-client"}
			}
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, `namespace app;
`+tc.schema+"\n")}}, options)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
