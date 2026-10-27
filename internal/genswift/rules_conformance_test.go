package genswift

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkexpr/conformance"
	"github.com/1homsi/onekit/internal/onktest"
)

const swiftRulesHarness = `import Foundation

while let line = readLine(strippingNewline: true) {
    if line.isEmpty { continue }
    let object = try JSONSerialization.jsonObject(with: Data(line.utf8))
    let subject = try Subject(json: object)
    let failed = subject.validate().filter { $0.range(of: "^(r[0-9]|field-)", options: .regularExpression) != nil }.sorted()
    print(failed.joined(separator: ","))
}
`

func TestGeneratedSwiftRulesMatchTheReferenceEvaluator(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
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
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(file)))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), swiftRulesHarness)
	bin := filepath.Join(dir, "check")
	compile := exec.Command("swiftc", "-warnings-as-errors", "-o", bin, "Onekit.swift", "Models.swift", "main.swift")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	run.Stdin = strings.NewReader(strings.Join(conformance.Inputs, "\n") + "\n")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d result lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("input %d (%s):\n  swift     %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func TestSwiftRulesRunInGeneratedModels(t *testing.T) {
	runSwiftSchema(t, `namespace app;

enum Tier {
  FREE,
  PRO,
}

model Customer {
  name: string;
}

@Onkt.rule("self.tier != 'FREE' || size(self.items) <= 1", "free orders hold one item")
@Onkt.rule("self.owner.name != ''", "an owner name is required")
@Onkt.rule("!has(self.coupon) || size(self.coupon) == 4", "coupons have four characters")
model Order {
  tier: Tier;
  owner: Customer;
  items: string[];
  coupon?: string;
  @Onkt.rule("value >= 0 && value <= 1000000", "total out of range")
  @encode(string)
  total: int64;
}
`, `
let good = Order(tier: .pro, owner: Customer(name: "a"), items: ["x", "y"], total: 10)
precondition(good.validate().isEmpty, "\(good.validate())")
let free = Order(tier: .free, owner: Customer(name: "a"), items: ["x", "y"])
precondition(free.validate() == ["free orders hold one item"], "\(free.validate())")
precondition(Order().validate().contains("an owner name is required"))
precondition(Order(owner: Customer(name: "a"), coupon: "abc").validate().contains("coupons have four characters"))
precondition(Order(owner: Customer(name: "a"), total: -1).validate().contains("total out of range"))
print("OK")
`)
}

func TestSwiftRulesCompileCleanlyForEveryFeatureSubset(t *testing.T) {
	for name, schema := range map[string]string{
		"macro only": `namespace app;

@Onkt.rule("self.xs.all(x, x > 0)", "positive")
model M {
  xs: int32[];
}
`,
		"matches only": `namespace app;

model M {
  @Onkt.rule("value.matches('[a-z]+')", "lower case")
  s: string;
}
`,
		"message list": `namespace app;

model I {
  n: int32;
}

@Onkt.rule("self.items.exists(i, i.n > 1)", "one item above one")
model M {
  items: I[];
}
`,
		"map and bytes": `namespace app;

@Onkt.rule("'k' in self.m && size(self.b) > 0", "map key and bytes")
model M {
  m: Record<int32>;
  b: bytes;
}
`,
		"enum list": `namespace app;

enum E {
  A,
  B,
}

@Onkt.rule("self.es.all(e, e == 'A')", "all A")
model M {
  es: E[];
}
`,
		"double only": `namespace app;

model M {
  @Onkt.rule("value * 2.0 < 10.0", "small")
  d: float64;
}
`,
		"deprecated": `namespace app;

@Onkt.rule("self.old > 0", "old is positive")
model M {
  #deprecated ""
  old: int32;
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			runSwiftSchema(t, schema, "print(\"OK\")\n")
		})
	}
}

func TestSwiftRulesWorkInsideANamespace(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	file := compileSwiftSchema(t, `namespace app;

@Onkt.rule("self.n > 0", "positive")
model M {
  n: int32;
}
`)
	dir := t.TempDir()
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypesInNamespace(file, "AppV1", nil)))
	writeSwiftFile(t, filepath.Join(dir, "Other.swift"), string(GenerateTypesInNamespace(file, "AppV2", nil)))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), "precondition(AppV1.M(n: 1).validate().isEmpty)\nprecondition(AppV2.M(n: 0).validate() == [\"positive\"])\nprint(\"OK\")\n")
	compile := exec.Command("swiftc", "-warnings-as-errors", "-o", filepath.Join(dir, "check"), "Onekit.swift", "Models.swift", "Other.swift", "main.swift")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("two namespaced files with rules do not compile together: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "check")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

func TestSwiftRulesReachMessagesFromAnotherNamespace(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	// The files compile together, as one TypeSpec project.
	var sources []onkcompile.Source
	for _, file := range onktest.Files(t, map[string]string{
		"common/money.tsp": `namespace common;

@Onkt.rule("self.cents >= 0", "no negative money")
model Money {
  @encode(string)
  cents: int64;
  #deprecated ""
  note: string;
}
`,
		"shop/order.tsp": `using common;


namespace shop;

@Onkt.rule("self.total.cents <= 1000 && self.total.note == ''", "too expensive")
model Order {
  total: Money;
}
`,
	}) {
		sources = append(sources, onkcompile.Source{Path: file.Path, AST: &file.File})
	}
	pkg, err := onkcompile.Compile(sources)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Common.swift"), string(GenerateTypesInNamespace(pkg.Files[0], "CommonV1", nil)))
	writeSwiftFile(t, filepath.Join(dir, "Shop.swift"), string(GenerateTypesInNamespace(pkg.Files[1], "ShopV1", testNamespaces{"Money": "CommonV1"})))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), `
precondition(ShopV1.Order(total: CommonV1.Money(cents: 5)).validate().isEmpty)
precondition(ShopV1.Order(total: CommonV1.Money(cents: 5000)).validate() == ["too expensive"])
precondition(CommonV1.Money(cents: -1).validate() == ["no negative money"])
print("OK")
`)
	compile := exec.Command("swiftc", "-o", filepath.Join(dir, "check"), "Onekit.swift", "Common.swift", "Shop.swift", "main.swift")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("cross-namespace rules do not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "check")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("run: %v\n%s", err, out)
	}
}
