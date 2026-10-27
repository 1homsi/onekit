package gents

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTS64BitRulesCompareExactly(t *testing.T) {
	runTSSchema(t, "namespace app;\n\nmodel A {\n  @Onkt.range(0, 9223372036854775806)\n  @encode(string)\n  n: int64;\n  @minValue(9007199254740993)\n  @encode(string)\n  u: uint64;\n  @Onkt.values(9007199254740993, 5)\n  @encode(string)\n  k: int64;\n  @maxValueExclusive(9007199254740993)\n  @minValueExclusive(-9007199254740993)\n  @encode(string)\n  l: int64;\n  @Onkt.range(0, 10)\n  plain: int32;\n  @Onkt.range(0, 100)\n  @encode(string)\n  oa?: int64;\n  @minValue(5)\n  @encode(string)\n  ob?: uint64;\n  @Onkt.values(1, 2)\n  oc?: int64;\n}\n\ninterface S {\n  @post\n  @route(\"/a\")\n  `op`(...A): A;\n}\n", `
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
check(has({ oa: "101" }, "oa violates @range") && !has({ oa: "100" }, "oa violates"), "an optional int64 is checked when set");
check(has({ ob: "4" }, "ob violates @gte") && has({ oc: 3 }, "oc must be one of") && !has({ oc: 2 }, "oc must be one of"), "optional uint64 and number-encoded int64");
console.log("OK");
`)
}

const ts64StrictSchema = "namespace app;\n\nmodel A {\n  @Onkt.range(0, 100)\n  @encode(string)\n  n: int64;\n  @Onkt.range(0, 100)\n  @encode(string)\n  oa?: int64;\n  @minValue(5)\n  @maxValueExclusive(9007199254740993)\n  @encode(string)\n  ob?: uint64;\n  @Onkt.values(1, 2)\n  oc?: int64;\n  @minValueExclusive(-1)\n  od: int64;\n}\n\ninterface S {\n  @post\n  @route(\"/a\")\n  `op`(...A): A;\n}\n"

func TestTS64BitRulesTypeCheckStrictly(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileTSSchema(t, ts64StrictSchema)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"],
    "types": []
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("64-bit rule checks do not type-check: %v\n%s", err, out)
	}
}
