package gents

import "testing"

func TestTSCodecsTolerateJSONNull(t *testing.T) {
	runTSSchema(t, "namespace app;\n\nmodel Sub {\n  x: string;\n}\n\nmodel Probe {\n  subs: Sub[];\n  `sub`: Sub;\n  by: Record<Sub>;\n  s: string;\n}\n\ninterface S {\n  @post\n  @route(\"/p\")\n  `op`(...Probe): Probe;\n}\n", `
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
check(validateProbe(null as any).length === 1 && validateProbe(undefined as any).length === 1, "validating null fails closed instead of passing or throwing");
check(validateProbe({ subs: [null] } as any).some((m) => m.includes("subs: value must be an object")), "a null element carries its own violation");
console.log("OK");
`)
}
