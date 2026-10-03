package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

const tsRulesHarness = `import { decodeSubject, validateSubject } from "./types.ts";
import { readFileSync } from "node:fs";

const lines = readFileSync(0, "utf8").split("\n").filter((line) => line.length > 0);
for (const line of lines) {
  const subject = decodeSubject(JSON.parse(line));
  const failed = validateSubject(subject).filter((m) => /^(r[0-9]|field-)/.test(m));
  failed.sort();
  console.log(failed.join(","));
}
`

func TestGeneratedTSRulesMatchTheReferenceEvaluator(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	_, file, _, err := conformance.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want, err := conformance.Expected()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "main.ts"), tsRulesHarness)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(strings.Join(conformance.Inputs, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d result lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("input %d (%s):\n  ts        %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func TestTSRulesFollowTheTypeScriptValueShapes(t *testing.T) {
	runTSSchema(t, `
package app

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message Address {
  street: string
  city: string
}

message Order
  @rule("self.billing.city == 'Paris' && size(self.billing.street) > 0", "bill a Paris address")
  @rule("self.amount > 5", "amount must exceed five")
  @rule("self.state == 'ACTIVE' && self.state_num == 'ACTIVE'", "both states must be active")
{
  billing: Address @flatten(prefix: "billing_")
  amount: int64 @encode(number)
  state: Status
  state_num: Status @encode(number)
}

message IdList @rule("size(self.ids) <= 2", "at most two ids") {
  ids: string[] @unwrap
}
`, `
import { validateOrder, validateIdList } from "./types.ts";

const good = { billingStreet: "1 Rue", billingCity: "Paris", amount: 7, state: "active", stateNum: 1 };
const goodViolations = validateOrder(good as any);
if (goodViolations.length !== 0) throw new Error("valid order rejected: " + goodViolations.join(", "));

const bad = validateOrder({ billingStreet: "", billingCity: "Rome", amount: 5, state: "UNSPECIFIED", stateNum: 0 } as any);
const expected = ["bill a Paris address", "amount must exceed five", "both states must be active"];
for (const message of expected) {
  if (!bad.includes(message)) throw new Error("missing violation " + message + ", got " + bad.join(", "));
}

if (validateIdList(["a", "b"] as any).length !== 0) throw new Error("two ids should pass");
if (!validateIdList(["a", "b", "c"] as any).includes("at most two ids")) throw new Error("three ids should fail");
console.log("OK");
`)
}

func TestGeneratedTSRulesTypeCheckStrictly(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	_, file, _, err := conformance.Compile()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noUnusedLocals": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated rules do not type-check: %v\n%s", err, out)
	}
}
