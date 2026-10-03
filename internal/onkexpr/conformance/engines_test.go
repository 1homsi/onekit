package conformance_test

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkexpr/conformance"
)

const nodeEngine = `
const input = JSON.parse(require("fs").readFileSync(0, "utf8"));
const out = input.patterns.map((p) => {
  const re = new RegExp("^(?:" + p + ")$", "u");
  return input.probes.map((s) => re.test(s));
});
console.log(JSON.stringify(out));
`

const pythonEngine = `
import json, re, sys, warnings
warnings.simplefilter("error")
data = json.load(sys.stdin)
out = []
for p in data["patterns"]:
    r = re.compile(p)
    out.append([r.fullmatch(s) is not None for s in data["probes"]])
print(json.dumps(out))
`

func runEngine(t *testing.T, name string, args []string, payload []byte) [][]bool {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available", name)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, out)
	}
	var matrix [][]bool
	if err := json.Unmarshal(out, &matrix); err != nil {
		t.Fatalf("%s output is not a match matrix: %v\n%s", name, err, out)
	}
	return matrix
}

func TestPortablePatternsBehaveTheSameInEveryEngine(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"patterns": conformance.Patterns, "probes": conformance.PatternProbes})
	if err != nil {
		t.Fatal(err)
	}
	want := make([][]bool, len(conformance.Patterns))
	for i, pattern := range conformance.Patterns {
		if err := onkexpr.ValidateRegex(pattern); err != nil {
			t.Fatalf("%q: %v", pattern, err)
		}
		re := regexp.MustCompile(`\A(?:` + pattern + `)\z`)
		want[i] = make([]bool, len(conformance.PatternProbes))
		for j, probe := range conformance.PatternProbes {
			want[i][j] = re.MatchString(probe)
		}
	}
	engines := map[string][][]bool{
		"node":    runEngine(t, "node", []string{"-e", nodeEngine}, payload),
		"python3": runEngine(t, "python3", []string{"-X", "utf8", "-c", pythonEngine}, payload),
	}
	for name, got := range engines {
		for i, pattern := range conformance.Patterns {
			for j, probe := range conformance.PatternProbes {
				if got[i][j] != want[i][j] {
					t.Errorf("%s disagrees with the reference on %q against %q: got %v, want %v", name, pattern, probe, got[i][j], want[i][j])
				}
			}
		}
	}
}
