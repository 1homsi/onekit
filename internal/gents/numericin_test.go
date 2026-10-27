package gents

import (
	"strings"
	"testing"
)

func TestTSIntegerInValidation(t *testing.T) {
	runTSSchema(t, `namespace app;

model Page {
  @Onkt.values(10, 25, 50)
  size: int32;
  @Onkt.values(1, 2)
  @encode(string)
  version?: int64;
}
`, `
import { validatePage } from "./types.ts";

if (validatePage({ size: 25, version: "2" } as any).length !== 0) throw new Error("valid page rejected");
if (validatePage({ size: 30 } as any).length !== 1) throw new Error("size 30 accepted");
if (validatePage({ size: 10, version: "3" } as any).length !== 1) throw new Error("version 3 accepted");
console.log("OK");
`)
}

func TestMSWUsesTheFirstAllowedIntegerValue(t *testing.T) {
	file := compileTSSchema(t, `namespace app;

model Page {
  @Onkt.values(25, 50)
  size: int32;
  @Onkt.values(3, 4)
  @encode(string)
  version: int64;
}

interface S {
  @get
  @route("/p")
  get(...Page): Page;
}
`)
	out := string(GenerateMSWHandlers(file))
	for _, want := range []string{`"size": 25`, `"version": "3"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in:\n%s", want, out)
		}
	}
}
