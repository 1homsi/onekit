package gents

import (
	"strings"
	"testing"
)

const int64WireSchema = `namespace wire;

@Onkt.discriminator("kind")
union BoxBody {
  @Onkt.tag("n") n: int64,
  @Onkt.tag("u") u: uint64,
  @Onkt.tag("label") label: string,
}

model Box {
  body: BoxBody;
  totals: Record<int64>;
  @Onkt.encode("string")
  ids: int64[];
  @encode(string)
  single: int64;
}

model Ask {
  id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  code: string;
}

interface Boxes {
  @get
  @route("/boxes/{id}")
  get(...Ask): Box | NotFound;

  @get
  @route("/watch/{id}")
  @Onkt.stream
  watch(...Ask): Box | NotFound;
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
	if !strings.Contains(out, `JSON.stringify(await midStreamErrorBody(req, err))`) || !strings.Contains(out, `return await response.json();`) {
		t.Fatalf("a typed error thrown mid-stream must reach clients as its declared payload:\n%s", out)
	}
}
