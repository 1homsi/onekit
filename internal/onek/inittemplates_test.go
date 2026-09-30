package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func initTemplateDir(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "todo-app-"+name)
}

func TestEveryInitTemplateBuildsAndVerifies(t *testing.T) {
	for _, template := range InitTemplates() {
		t.Run(template.Name, func(t *testing.T) {
			dir := initTemplateDir(t, template.Name)
			if err := InitTemplateProject(dir, false, template.Name); err != nil {
				t.Fatalf("init: %v", err)
			}
			summary, err := BuildWithSummary(dir)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			cfg, err := LoadConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(summary.Targets) == 0 || summary.Files == 0 {
				t.Fatalf("template generated nothing: %+v", summary)
			}
			if len(summary.Targets) != len(cfg.EnabledTargets()) {
				t.Fatalf("summary targets %v != config targets %v", summary.Targets, cfg.EnabledTargets())
			}
			if err := VerifyGenerated(dir); err != nil {
				t.Fatalf("verify: %v", err)
			}
			if err := Check(dir); err != nil {
				t.Fatalf("check: %v", err)
			}
		})
	}
}

func TestInitTemplatesDescribeTheTargetsTheyConfigure(t *testing.T) {
	want := map[string][]string{
		"go":        {"go-server", "go-client", "openapi"},
		"web":       {"go-server", "go-client", "ts-client", "openapi"},
		"fullstack": {"go-server", "go-client", "ts-client", "dart-client", "swift-client", "openapi"},
		"mobile":    {"dart-client", "swift-client", "openapi"},
		"ts":        {"ts-client", "ts-server", "openapi"},
		"rust":      {"rust-client", "rust-server", "openapi"},
	}
	for _, template := range InitTemplates() {
		dir := initTemplateDir(t, template.Name)
		if err := InitTemplateProject(dir, false, template.Name); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(cfg.EnabledTargets(), ",")
		if got != strings.Join(want[template.Name], ",") && !sameTargets(cfg.EnabledTargets(), want[template.Name]) {
			t.Errorf("%s targets = %s, want %v", template.Name, got, want[template.Name])
		}
	}
	if len(InitTemplates()) != len(want) {
		t.Fatalf("every template needs an entry in this test: %d templates, %d entries", len(InitTemplates()), len(want))
	}
}

func sameTargets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := map[string]bool{}
	for _, name := range got {
		set[name] = true
	}
	for _, name := range want {
		if !set[name] {
			return false
		}
	}
	return true
}

func TestGoInitTemplatesProduceCompilableGo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	for _, name := range []string{"web", "fullstack"} {
		t.Run(name, func(t *testing.T) {
			dir := initTemplateDir(t, name)
			if err := InitTemplateProject(dir, false, name); err != nil {
				t.Fatal(err)
			}
			if err := Build(dir); err != nil {
				t.Fatal(err)
			}
			gen := filepath.Join(dir, "gen", "go")
			module := "example.com/" + filepath.Base(dir) + "/gen/go"
			writeTestFile(t, filepath.Join(gen, "go.mod"), "module "+module+"\n\ngo 1.26\n")
			cmd := exec.Command("go", "vet", "./...")
			cmd.Dir = gen
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated Go does not vet: %v\n%s", err, out)
			}
		})
	}
}

func TestSwiftInitTemplatesTypeCheck(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	for _, name := range []string{"fullstack", "mobile"} {
		t.Run(name, func(t *testing.T) {
			dir := initTemplateDir(t, name)
			if err := InitTemplateProject(dir, false, name); err != nil {
				t.Fatal(err)
			}
			if err := Build(dir); err != nil {
				t.Fatal(err)
			}
			var files []string
			root := filepath.Join(dir, "ios", "Sources", "Api")
			if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && filepath.Ext(path) == ".swift" {
					files = append(files, path)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("swiftc", append([]string{"-typecheck", "-warnings-as-errors"}, files...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("swiftc: %v\n%s", err, out)
			}
		})
	}
}

func TestInitRejectsUnknownTemplatesBeforeWritingAnything(t *testing.T) {
	dir := initTemplateDir(t, "nope")
	err := InitTemplateProject(dir, false, "nonexistent")
	if err == nil || !strings.Contains(err.Error(), `unknown init template "nonexistent"`) || !strings.Contains(err.Error(), "fullstack") {
		t.Fatalf("expected an error naming the available templates, got %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("nothing may be created for an unknown template: %v", statErr)
	}
}

func TestInitDefaultsToTheGoTemplate(t *testing.T) {
	dir := initTemplateDir(t, "default")
	if err := Init(dir, false); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(dir, "onekit.toml"))
	if err != nil || !strings.Contains(string(config), "[generate.go-server]") || strings.Contains(string(config), "ts-client") {
		t.Fatalf("default template changed: %v\n%s", err, config)
	}
}

func TestInitTemplatesStayUnopinionatedAboutFrontendLibraries(t *testing.T) {
	for _, template := range InitTemplates() {
		dir := initTemplateDir(t, template.Name)
		if err := InitTemplateProject(dir, false, template.Name); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ts := cfg.Generate.TSClient; ts != nil && (ts.Zod || ts.ReactQuery || ts.MSW) {
			t.Errorf("%s template enables a frontend library by default: %+v", template.Name, *ts)
		}
	}
}

func TestPlainTypeScriptTemplateOutputNeedsOnlyTheTypeScriptCompiler(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	dir := initTemplateDir(t, "plainweb")
	if err := InitTemplateProject(dir, false, "web"); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","strict":true,"noEmit":true,"lib":["ES2022","DOM"]},"include":["web/src/api/*.ts"]}`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the starter's TypeScript must compile with no packages installed: %v\n%s", err, out)
	}
}
