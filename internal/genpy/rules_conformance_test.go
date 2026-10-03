package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

const pyRulesHarness = `import json
import re
import sys

from models import Subject

for line in sys.stdin.read().split("\n"):
    if not line:
        continue
    subject = Subject.from_dict(json.loads(line))
    failed = []
    try:
        subject.validate()
    except ValueError as error:
        failed = [m for m in str(error).split("; ") if re.match(r"^(r[0-9]|field-)", m)]
    print(",".join(sorted(failed)))
`

func TestGeneratedPythonRulesMatchTheReferenceEvaluator(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
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
	writeFile(t, filepath.Join(dir, "models.py"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "main.py"), pyRulesHarness)
	cmd := exec.Command("python3", "-W", "error", "main.py")
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
			t.Errorf("input %d (%s):\n  python    %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func TestPythonRulesRunInGeneratedModels(t *testing.T) {
	runPythonSchema(t, `
package app

enum Tier { FREE PRO }

message Customer {
  name: string
  tags: string[]
}

message Order
  @rule("self.tier != 'FREE' || size(self.items) <= 1", "free orders hold one item")
  @rule("self.owner.name != ''", "an owner name is required")
  @rule("!has(self.coupon) || size(self.coupon) == 4", "coupons have four characters")
{
  tier: Tier
  owner: Customer
  items: string[]
  coupon: string?
  total: int64 @rule("value >= 0 && value <= 1000000", "total out of range")
}
`, `
from models import Order, Customer, Tier

def violations(order):
    try:
        order.validate()
        return []
    except ValueError as error:
        return str(error).split("; ")

good = Order(tier=Tier.PRO, owner=Customer(name="a"), items=["x", "y"], total=10)
assert violations(good) == [], violations(good)

assert violations(Order(tier=Tier.FREE, owner=Customer(name="a"), items=["x", "y"])) == ["free orders hold one item"]
assert "an owner name is required" in violations(Order())
assert "an owner name is required" in violations(Order(owner=Customer(name="")))
assert "coupons have four characters" in violations(Order(owner=Customer(name="a"), coupon="abc"))
assert "total out of range" in violations(Order(owner=Customer(name="a"), total=-1))
assert "total out of range" in violations(Order(owner=Customer(name="a"), total=2 ** 63 - 1))
print("OK")
`)
}
