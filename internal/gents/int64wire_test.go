package gents

import (
	"strings"
	"testing"
)

const int64WireSchema = `
package wire

message Box {
  body: oneof(discriminator: "kind") {
    n: int64 @tag("n")
    u: uint64 @tag("u")
    label: string @tag("label")
  }
  totals: map[string, int64]
  ids: int64[]
  single: int64
}

message Ask { id: string }
message NotFound @status(404) { code: string }

service Boxes {
  get(Ask) -> Box | NotFound @get("/boxes/{id}")
  watch(Ask) -> Box | NotFound @get("/watch/{id}") @stream
}
`

func TestTSOneofInt64VariantsAreStringsLikeTheWire(t *testing.T) {
	out := string(GenerateTypes(compileTSSchema(t, int64WireSchema)))
	for _, want := range []string{`{ kind: "n"; n: string }`, `{ kind: "u"; u: string }`, `{ kind: "label"; label: string }`} {
		if !strings.Contains(out, want) {
			t.Fatalf("oneof variant type missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `n: number`) || strings.Contains(out, `u: number`) {
		t.Fatalf("int64/uint64 oneof variants must not be typed as number:\n%s", out)
	}
}

func TestTSMapInt64ValuesAreNumbersInTypesAndValidator(t *testing.T) {
	file := compileTSSchema(t, int64WireSchema)
	types := string(GenerateTypes(file))
	if !strings.Contains(types, `totals?: Record<string, number>`) {
		t.Fatalf("map int64 values are JSON numbers on the wire, like Go, Rust, Python and Dart:\n%s", types)
	}
	if !strings.Contains(types, `Object.values(v.totals).every((item: any) => typeof item === "number" && Number.isSafeInteger(item))`) {
		t.Fatalf("the validator must accept numeric map int64 values:\n%s", types)
	}
}

func TestTSServerStreamsTypedErrorPayloads(t *testing.T) {
	out := string(GenerateServer(compileTSSchema(t, int64WireSchema)))
	if !strings.Contains(out, `const errBody = err instanceof HttpError ? err.body : { message: "internal server error" };`) {
		t.Fatalf("a typed error thrown mid-stream must reach clients as its declared payload:\n%s", out)
	}
}
