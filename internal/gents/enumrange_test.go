package gents

import "testing"

func TestTSNumberEnumRejectsUndeclaredValues(t *testing.T) {
	runTSSchema(t, `
package app
enum Level { LOW  MID  HIGH }
message M { level: Level @encode(number) }
`, `
import { validateM } from "./types.ts";

if (validateM({ level: 2 } as any).length !== 0) throw new Error("valid value rejected");
for (const level of [3, -1, 1.5]) {
  if (validateM({ level } as any).length === 0) throw new Error("undeclared value accepted: " + level);
}
console.log("OK");
`)
}
