package gents

import "testing"

func TestTSLenCountsCharacters(t *testing.T) {
	runTSSchema(t, `
package app
message Name { value: string @len(1, 2) }
`, `
import { validateName } from "./types.ts";

if (validateName({ value: "😀😀" }).length !== 0) throw new Error("two emoji rejected");
if (validateName({ value: "😀😀😀" }).length === 0) throw new Error("three emoji accepted");
console.log("OK");
`)
}
