package gents

import "testing"

func TestTSRequiredOneof(t *testing.T) {
	runTSSchema(t, `namespace app;

model A {
  v: string;
}

union MP {
  a: A,
}

model M {
  @Onkt.required
  p: MP;
}
`, `
import { validateM } from "./types.ts";

if (!validateM({} as any).includes("p is required")) throw new Error("missing oneof passed");
if (validateM({ p: { type: "a", a: { v: "x" } } } as any).length !== 0) throw new Error("set oneof rejected");
console.log("OK");
`)
}
