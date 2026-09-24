package gents

import "testing"

func TestTSRequiredRejectsEmptyCollections(t *testing.T) {
	runTSSchema(t, `
package app
message M {
  ids: string[] @required
  labels: map[string, string] @required
}
`, `
import { validateM } from "./types.ts";

const empty = validateM({ ids: [], labels: {} });
if (!empty.includes("ids is required") || !empty.includes("labels is required")) throw new Error("empty collections passed: " + empty.join(", "));
if (validateM({ ids: ["a"], labels: { k: "v" } }).length !== 0) throw new Error("filled collections rejected");
console.log("OK");
`)
}
