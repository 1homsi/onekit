package gents

import "testing"

func TestTSCodecsTolerateJSONNull(t *testing.T) {
	runTSSchema(t, `
package app
message Sub { x: string }
message Probe {
  subs: Sub[]
  sub: Sub
  by: map[string, Sub]
  s: string
}
service S { op(Probe) -> Probe @post("/p") }
`, `
import { decodeProbe, encodeProbe, validateProbe } from "./types.ts";

const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };
for (const wire of [{ subs: null }, { sub: null }, { by: null }, { subs: [null] }, { by: { a: null } }, { sub: null, s: "a" }]) {
  const decoded = decodeProbe(wire as any);
  encodeProbe(decoded);
  validateProbe(decoded);
}
check(JSON.stringify(encodeProbe(decodeProbe({ sub: null } as any))) === '{"sub":null}', "a null message field stays null on the wire");
check(JSON.stringify(encodeProbe(decodeProbe({ subs: null } as any))) === '{"subs":null}', "a null list stays null");
check(validateProbe({ subs: [null] } as any).some((m) => m.includes("subs has invalid type")), "a null element is reported, not thrown");
check(validateProbe(null as any).length === 0, "validating null does not throw");
console.log("OK");
`)
}
