package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibilityAgainstGitRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	project := filepath.Join(repo, "api")
	writeTestFile(t, filepath.Join(project, "api.onk"), "message R { id: string  name: string }\nservice S { get(R) -> R @put(\"/r/{id}\") }\n")
	git("add", "-A")
	git("commit", "-q", "-m", "baseline")
	writeTestFile(t, filepath.Join(project, "api.onk"), "message R { id: string }\nservice S { get(R) -> R @put(\"/r/{id}\") }\n")
	findings, err := CompatibilityAgainstRef("HEAD", project)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Path, "R.name") {
		t.Fatalf("want removed field, got %+v", findings)
	}
}

// TestCompatibilityAgainstGitRefReadsTypeSpec compares a TypeSpec project
// with its committed baseline, which is checked out without node_modules.
func TestCompatibilityAgainstGitRefReadsTypeSpec(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	linkTypeSpec(t, dir)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeTestFile(t, filepath.Join(dir, ".gitignore"), "node_modules\n")
	writeTestFile(t, filepath.Join(dir, "onkt.toml"), "module = \"example.com/compat\"\n")
	writeTestFile(t, filepath.Join(dir, "main.tsp"), "import \"@typespec/http\";\nimport \"@onkt/typespec\";\nnamespace api;\nmodel Item { id: string; count: int32; }\n")
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	writeTestFile(t, filepath.Join(dir, "main.tsp"), "import \"@typespec/http\";\nimport \"@onkt/typespec\";\nnamespace api;\nmodel Item { id: string; }\n")

	findings, err := CompatibilityAgainstRef("HEAD", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Path != "api.Item.count" {
		t.Fatalf("findings = %+v, want the removed field", findings)
	}
}
