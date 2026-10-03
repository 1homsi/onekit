package gendart

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

const dartRulesHarness = `import 'dart:convert';
import 'dart:io';

import 'package:onekit_check/models.dart';

void main() {
  final lines = stdin.transform(utf8.decoder).join().then((text) {
    for (final line in text.split('\n')) {
      if (line.isEmpty) continue;
      final subject = Subject.fromJson(jsonDecode(line) as Map<String, dynamic>);
      final failed = subject.validate().where((m) => RegExp(r'^(r[0-9]|field-)').hasMatch(m)).toList()..sort();
      print(failed.join(','));
    }
  });
  lines.then((_) {});
}
`

func TestGeneratedDartRulesMatchTheReferenceEvaluator(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart not available")
	}
	_, file, _, err := conformance.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want, err := conformance.Expected()
	if err != nil {
		t.Fatal(err)
	}
	dir := dartPackage(t, file)
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), dartRulesHarness)
	cmd := exec.Command("dart", "run", "bin/main.dart")
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
			t.Errorf("input %d (%s):\n  dart      %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func TestDartRulesRunInGeneratedModels(t *testing.T) {
	runDartSchema(t, `
package app

enum Tier { FREE PRO }

message Customer {
  name: string
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
import 'package:onekit_check/models.dart';

void main() {
  final good = Order(tier: Tier.pro, owner: Customer(name: 'a'), items: ['x', 'y'], total: 10);
  assert(good.validate().isEmpty);
  final free = Order(tier: Tier.free, owner: Customer(name: 'a'), items: ['x', 'y']);
  if (free.validate().join('|') != 'free orders hold one item') throw 'free: ${free.validate()}';
  if (!Order().validate().contains('an owner name is required')) throw 'unset owner';
  if (!Order(owner: Customer(name: 'a'), coupon: 'abc').validate().contains('coupons have four characters')) throw 'coupon';
  if (!Order(owner: Customer(name: 'a'), total: -1).validate().contains('total out of range')) throw 'negative total';
  print('OK');
}
`)
}

func TestDartRulesAnalyzeCleanlyForEveryFeatureSubset(t *testing.T) {
	for name, schema := range map[string]string{
		"macro only":    "package app\nmessage M @rule(\"self.xs.all(x, x > 0)\", \"positive\") {\n  xs: int32[]\n}\n",
		"matches only":  "package app\nmessage M {\n  s: string @rule(\"value.matches('[a-z]+')\", \"lower case\")\n}\n",
		"message list":  "package app\nmessage I {\n  n: int32\n}\nmessage M @rule(\"self.items.exists(i, i.n > 1)\", \"one item above one\") {\n  items: I[]\n}\n",
		"map and bytes": "package app\nmessage M @rule(\"'k' in self.m && size(self.b) > 0\", \"map key and bytes\") {\n  m: map[string, int32]\n  b: bytes\n}\n",
		"enum list":     "package app\nenum E { A B }\nmessage M @rule(\"self.es.all(e, e == 'A')\", \"all A\") {\n  es: E[]\n}\n",
		"double only":   "package app\nmessage M {\n  d: float64 @rule(\"value * 2.0 < 10.0\", \"small\")\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dartPackage(t, compileDartSchema(t, schema))
		})
	}
}
