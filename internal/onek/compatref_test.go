package onek

import (
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
	writeTestFile(t, filepath.Join(project, "api.onk"), "message R { id: string  name: string }\nservice S { get(R) -> R @get(\"/r/{id}\") }\n")
	git("add", "-A")
	git("commit", "-q", "-m", "baseline")
	writeTestFile(t, filepath.Join(project, "api.onk"), "message R { id: string }\nservice S { get(R) -> R @get(\"/r/{id}\") }\n")
	findings, err := CompatibilityAgainstRef("HEAD", project)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Path, "R.name") {
		t.Fatalf("want removed field, got %+v", findings)
	}
}
