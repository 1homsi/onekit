package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

const rustRulesCargoToml = `
[package]
name = "NAME"
version = "0.1.0"
edition = "2024"

[dependencies]
base64 = "0.22"
regex = "1"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
url = "2"
uuid = "1"
validator = "0.20"
`

const rustRulesHarness = `use onekit_rules_conformance::Subject;
use std::io::BufRead;

fn main() {
    for line in std::io::stdin().lock().lines() {
        let line = line.unwrap();
        if line.is_empty() {
            continue;
        }
        let subject: Subject = match serde_json::from_str(&line) {
            Ok(value) => value,
            Err(error) => {
                println!("DECODE ERROR: {error}");
                continue;
            }
        };
        let mut failed: Vec<String> = subject
            .rule_violations()
            .into_iter()
            .map(|violation| violation.message)
            .filter(|message| message.starts_with('r') || message.starts_with("field-"))
            .collect();
        failed.sort();
        println!("{}", failed.join(","));
    }
}
`

func TestGeneratedRustRulesMatchTheReferenceEvaluator(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
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
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRustFile(t, filepath.Join(dir, "Cargo.toml"), strings.Replace(rustRulesCargoToml, "NAME", "onekit-rules-conformance", 1))
	writeRustFile(t, filepath.Join(dir, "src", "lib.rs"), string(GenerateTypes(file)))
	writeRustFile(t, filepath.Join(dir, "src", "main.rs"), rustRulesHarness)

	cmd := cargoCommand(dir, "run", "--quiet")
	cmd.Stdin = strings.NewReader(strings.Join(conformance.Inputs, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated crate failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d result lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("input %d (%s):\n  rust      %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func writeRustFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const rustRulesValidateSchema = `
package app

enum Tier { FREE PRO }

message Customer {
  name: string
}

message Order
  @rule("self.tier != 'FREE' || size(self.items) <= 1", "free orders hold one item")
  @rule("self.owner.name != ''", "an owner name is required")
{
  tier: Tier
  owner: Customer
  items: string[]
  total: int64 @rule("value >= 0 && value <= 1000000", "total out of range")
}
`

const rustRulesValidateHarness = `

#[cfg(test)]
mod rule_tests {
    use super::*;

    fn owner(name: &str) -> Option<Box<Customer>> {
        Some(Box::new(Customer { name: name.to_string() }))
    }

    #[test]
    fn validate_reports_the_first_failed_rule_with_its_field() {
        let good = Order { tier: Tier::Pro, owner: owner("a"), items: vec!["x".into(), "y".into()], total: 10 };
        assert!(good.validate().is_ok());
        assert!(good.rule_violations().is_empty());

        let negative = Order { total: -1, ..good.clone() };
        let error = negative.validate().unwrap_err();
        assert_eq!(error.field, "total");
        assert_eq!(error.message, "total out of range");

        let free = Order { tier: Tier::Free, ..good.clone() };
        let error = free.validate().unwrap_err();
        assert_eq!(error.field, "");
        assert_eq!(error.message, "free orders hold one item");
    }

    #[test]
    fn unset_messages_read_as_zero_values_and_every_failure_is_listed() {
        let nobody = Order { owner: None, total: i64::MAX, ..Default::default() };
        let messages: Vec<String> = nobody.rule_violations().into_iter().map(|v| v.message).collect();
        assert_eq!(messages, vec!["an owner name is required".to_string(), "total out of range".to_string()]);
        let blank = Order { owner: owner(""), ..Default::default() };
        assert_eq!(blank.rule_violations().len(), 1);
    }
}
`

func TestRustRulesIntegrateWithValidate(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	file := compileRustSchema(t, rustRulesValidateSchema)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRustFile(t, filepath.Join(dir, "Cargo.toml"), strings.Replace(rustRulesCargoToml, "NAME", "onekit-rules-validate", 1))
	writeRustFile(t, filepath.Join(dir, "src", "lib.rs"), string(GenerateTypes(file))+rustRulesValidateHarness)
	out, err := cargoCommand(dir, "test", "--quiet").CombinedOutput()
	if err != nil {
		t.Fatalf("generated rules failed: %v\n%s", err, out)
	}
}
