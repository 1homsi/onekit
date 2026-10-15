package gents

import "testing"

func TestTS64BitRulesCompareExactly(t *testing.T) {
	runTSSchema(t, `
package app
message A {
  n: int64 @range(0, 9223372036854775806)
  u: uint64 @gte(9007199254740993)
  k: int64 @in(9007199254740993, 5)
  l: int64 @lt(9007199254740993) @gt(-9007199254740993)
  plain: int32 @range(0, 10)
}
service S { op(A) -> A @post("/a") }
`, `
import { validateA } from "./types.ts";

const check = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };
const has = (v: any, text: string) => validateA(v).some((m) => m.includes(text));

check(has({ n: "9223372036854775807" }, "n violates @range"), "the largest int64 is above the range");
check(!has({ n: "9223372036854775806" }, "n violates"), "the upper bound itself is allowed");
check(has({ n: "-1" }, "n violates @range"), "below the range");
check(has({ u: "9007199254740992" }, "u violates @gte"), "2^53 is below 2^53+1 and must fail @gte");
check(!has({ u: "9007199254740993" }, "u violates"), "2^53+1 meets @gte(2^53+1)");
check(has({ k: "9007199254740992" }, "k must be one of"), "2^53 is not in the set");
check(!has({ k: "9007199254740993" }, "k must be one of"), "2^53+1 is in the set");
check(!has({ k: "5" }, "k must be one of"), "5 is in the set");
check(has({ l: "9007199254740993" }, "l violates @lt"), "@lt compares exactly");
check(has({ l: "-9007199254740993" }, "l violates @gt"), "@gt compares exactly");
check(!has({ n: "not a number" }, "n violates"), "a malformed value reports its type, not a rule");
check(validateA({ n: "not a number" }).some((m) => m.includes("n has invalid type")), "a malformed value is reported");
check(has({ plain: 11 }, "plain violates @range") && !has({ plain: 10 }, "plain violates"), "a 32-bit field is unchanged");
check(validateA({}).length === 0, "absent fields pass");
console.log("OK");
`)
}
