package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkexpr/conformance"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const goRulesHarness = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	conf "example.com/rules/conf"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var subject conf.Subject
		if err := json.Unmarshal(scanner.Bytes(), &subject); err != nil {
			fmt.Println("DECODE ERROR: " + err.Error())
			continue
		}
		var failed []string
		if err := subject.Validate(); err != nil {
			failed = err.(*conf.ValidationErrors).ViolationList()
		}
		var rules []string
		for _, f := range failed {
			if strings.HasPrefix(f, "r") || strings.HasPrefix(f, "field-") {
				rules = append(rules, f)
			}
		}
		sort.Strings(rules)
		fmt.Println(strings.Join(rules, ","))
	}
}
`

func TestGeneratedGoRulesMatchTheReferenceEvaluator(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	_, file, _, err := conformance.Compile()
	if err != nil {
		t.Fatal(err)
	}
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := GenerateValidation(file)
	if err != nil {
		t.Fatal(err)
	}
	want, err := conformance.Expected()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/rules\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "conf", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "conf", "validate.go"), string(validation))
	writeFile(t, filepath.Join(dir, "main.go"), goRulesHarness)

	cmd := exec.Command("go", "run", ".")
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
			t.Errorf("input %d (%s):\n  go        %s\n  reference %s", i, conformance.Inputs[i], got[i], want[i])
		}
	}
}

func TestGeneratedGoRulesCompileWithoutOptionalFeatures(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	for name, schema := range map[string]string{
		"field rule only":   "package small\nmessage M {\n  n: int32 @rule(\"value >= 0\", \"n must not be negative\")\n}\n",
		"message rule only": "package small\nmessage M @rule(\"self.a < self.b\", \"a before b\") {\n  a: int32\n  b: int32\n}\n",
		"macro":             "package small\nmessage M @rule(\"self.xs.all(x, x > 0)\", \"positive\") {\n  xs: int32[]\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := compileFixtureSource(t, schema)
			types, err := GenerateTypesWithResolver(file, nil)
			if err != nil {
				t.Fatal(err)
			}
			validation, err := GenerateValidation(file)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/small\n\ngo 1.26\n")
			writeFile(t, filepath.Join(dir, "types.go"), string(types))
			writeFile(t, filepath.Join(dir, "validate.go"), string(validation))
			cmd := exec.Command("go", "vet", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated code does not vet: %v\n%s\n%s", err, out, validation)
			}
		})
	}
}

func compileFixtureSource(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return pkg.Files[0]
}
