package onek

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const reservedWordsSchema = `package app

message Q {
  in: string
  from: string
  default: string
  type: string
  class: string
  enum: string
  import: string
  none: string
  return: string
}
message Out { q: Q }

service S {
  base_path: "/v1"
  get(Q) -> Out @get("/q")
}
`

func reservedWordsProject(t *testing.T, targets string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/x\"\n\n"+targets)
	writeTestFile(t, filepath.Join(dir, "app.onk"), reservedWordsSchema)
	return dir
}

func TestWireNamesThatAreKeywordsOnlyMatterForTargetsThatNeedThem(t *testing.T) {
	dir := reservedWordsProject(t, `
[generate.go-server]
out = "gen/go"

[generate.go-client]
out = "gen/go"

[generate.ts-client]
out = "gen/ts"

[generate.ts-server]
out = "gen/tsserver"
`)
	if err := Build(dir); err != nil {
		t.Fatalf("a project without a Python target must accept Python keywords as member names: %v", err)
	}
	types := read(t, filepath.Join(dir, "gen", "go", "types.gen.go"))
	for _, want := range []string{"In ", `json:"in,omitempty"`, `json:"from,omitempty"`, `json:"default,omitempty"`, `json:"type,omitempty"`, `json:"class,omitempty"`, `json:"enum,omitempty"`, `json:"import,omitempty"`} {
		if !strings.Contains(types, want) {
			t.Errorf("Go types lack %q:\n%s", want, types)
		}
	}
	ts := read(t, filepath.Join(dir, "gen", "ts", "types.ts"))
	for _, want := range []string{"in?: string", "from?: string", "default?: string", "type?: string", "class?: string", "enum?: string", "import?: string"} {
		if !strings.Contains(ts, want) {
			t.Errorf("TypeScript types lack %q:\n%s", want, ts)
		}
	}
	if _, err := exec.LookPath("tsc"); err == nil {
		writeTestFile(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","strict":true,"noEmit":true,"lib":["ES2022","DOM"]},"files":["gen/ts/types.ts","gen/ts/client.ts"]}`)
		cmd := exec.Command("tsc", "-p", "tsconfig.json")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the TypeScript must compile: %v\n%s", err, out)
		}
	}
	if _, err := exec.LookPath("go"); err == nil {
		writeTestFile(t, filepath.Join(dir, "gen", "go", "go.mod"), "module example.com/x\n\ngo 1.27\n")
		cmd := exec.Command("go", "vet", "./...")
		cmd.Dir = filepath.Join(dir, "gen", "go")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the Go must compile: %v\n%s", err, out)
		}
	}
}

func TestPythonTargetStillRejectsPythonKeywordMembers(t *testing.T) {
	dir := reservedWordsProject(t, "[generate.go-server]\nout = \"gen/go\"\n\n[generate.python-client]\nout = \"gen/py\"\n")
	err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), "reserved in Python") {
		t.Fatalf("expected the Python keyword error, got %v", err)
	}
}

func TestProjectWithoutTargetsKeepsEveryNamingRule(t *testing.T) {
	dir := reservedWordsProject(t, "")
	if err := Check(dir); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("with no targets listed every rule still applies, got %v", err)
	}
}
