package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// compatBaseSchema is a complete TypeSpec file: the action runs the real
// TypeSpec compiler.
const compatBaseSchema = `import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace api;

model Item {
  id: string;
  count: int32;
}

model GetItemRequest {
  id: string;
}

@route("/items/v1")
interface Items {
  @get
  @route("/items/{id}")
  getItem(...GetItemRequest): Item;
}
`

type compatActionEnv struct {
	onkt   string
	script string
	dir    string
}

func setupCompatAction(t *testing.T) compatActionEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the action scripts target bash runners")
	}
	for _, tool := range []string{"bash", "git", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	root := t.TempDir()
	onkt := filepath.Join(root, "onkt")
	build := exec.Command("go", "build", "-o", onkt, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build onkt: %v\n%s", err, out)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "compat-report.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"onkt.toml":        "module = \"example.com/act\"\n\n[generate.go-server]\nout = \"./gen/go\"\n\n[generate.dart-client]\nout = \"./gen/dart\"\n",
		"api/items.tsp":    compatBaseSchema,
		".gitignore":       "gen/\nnode_modules\n",
		"api/.gitkeep.txt": "",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The report compiles TypeSpec with the project's own toolchain.
	linkTypeSpec(t, dir)
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	return compatActionEnv{onkt: onkt, script: script, dir: dir}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func (e compatActionEnv) run(t *testing.T, against, allow string) (string, string, error) {
	t.Helper()
	tmp := t.TempDir()
	reportFile := filepath.Join(tmp, "report.md")
	outputFile := filepath.Join(tmp, "output.txt")
	cmd := exec.Command("bash", e.script)
	cmd.Env = append(os.Environ(),
		"ONKT_BIN="+e.onkt,
		"ONKT_DIR="+e.dir,
		"ONKT_AGAINST="+against,
		"ONKT_ALLOW="+allow,
		"REPORT_FILE="+reportFile,
		"GITHUB_OUTPUT="+outputFile,
		"GITHUB_STEP_SUMMARY="+filepath.Join(tmp, "summary.md"),
	)
	cmd.Dir = e.dir
	_, err := cmd.CombinedOutput()
	reportBytes, _ := os.ReadFile(reportFile)
	outputBytes, _ := os.ReadFile(outputFile)
	return string(reportBytes), string(outputBytes), err
}

func (e compatActionEnv) breakSchema(t *testing.T) {
	t.Helper()
	path := filepath.Join(e.dir, "api", "items.tsp")
	schema := strings.Replace(compatBaseSchema, "count: int32", "total: int32", 1)
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompatActionReportsBreakingChanges(t *testing.T) {
	env := setupCompatAction(t)
	env.breakSchema(t)
	report, outputs, err := env.run(t, "main", "")
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	for _, want := range []string{"<!-- onek-compat -->", "**1 breaking change(s)**", "`api.Item.count` | field was removed", "`go-server`, `dart-client`"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report is missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(outputs, "breaking=1") {
		t.Fatalf("expected breaking=1, got %q", outputs)
	}
}

func TestCompatActionStaysQuietWhenNothingBreaks(t *testing.T) {
	env := setupCompatAction(t)
	report, outputs, err := env.run(t, "main", "")
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	if !strings.Contains(report, "No breaking changes against `main`") {
		t.Fatalf("unexpected report:\n%s", report)
	}
	if !strings.Contains(outputs, "breaking=0") {
		t.Fatalf("expected breaking=0, got %q", outputs)
	}
}

func TestCompatActionHonoursAllowedPaths(t *testing.T) {
	env := setupCompatAction(t)
	env.breakSchema(t)
	_, outputs, err := env.run(t, "main", "  api.Item  \n\n")
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	if !strings.Contains(outputs, "breaking=0") {
		t.Fatalf("allowed changes must not count as breaking, got %q", outputs)
	}
}

func TestCompatActionFailsOnAnUnknownRef(t *testing.T) {
	env := setupCompatAction(t)
	if _, _, err := env.run(t, "no-such-ref", ""); err == nil {
		t.Fatal("an unresolvable base ref must fail the script")
	}
}
