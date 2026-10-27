package gents

import "testing"

func TestTSLenCountsCharacters(t *testing.T) {
	runTSSchema(t, `namespace app;

model Name {
  @minLength(1)
  @maxLength(2)
  value: string;
}
`, `
import { validateName } from "./types.ts";

if (validateName({ value: "😀😀" }).length !== 0) throw new Error("two emoji rejected");
if (validateName({ value: "😀😀😀" }).length === 0) throw new Error("three emoji accepted");
console.log("OK");
`)
}
