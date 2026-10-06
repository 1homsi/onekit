package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTSSharedProject(t *testing.T, tsTarget string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app\"\n\n[generate.ts-client]\nout = \"web/api\"\n"+tsTarget)
	writeTestFile(t, filepath.Join(dir, "orders", "orders.onk"), `package orders

message Order { id: string }

service Orders {
  base_path: "/v1"
  get(Order) -> Order @get("/orders/{id}")
}
`)
	writeTestFile(t, filepath.Join(dir, "billing", "invoices", "invoices.onk"), `package invoices

message Invoice { id: string }

service Invoices {
  base_path: "/v1"
  get(Invoice) -> Invoice @get("/invoices/{id}")
}
`)
	return dir
}

func TestTSSharedRuntimeBuildsOneRuntimeForEveryPackage(t *testing.T) {
	dir := writeTSSharedProject(t, "runtime = \"shared\"\n")
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	out := filepath.Join(dir, "web", "api")
	if _, err := os.Stat(filepath.Join(out, "onekitrt", "runtime.ts")); err != nil {
		t.Fatalf("the shared runtime was not written: %v", err)
	}
	for path, want := range map[string]string{
		filepath.Join(out, "orders", "client.ts"):              `from "../onekitrt/runtime.js"`,
		filepath.Join(out, "billing", "invoices", "client.ts"): `from "../../onekitrt/runtime.js"`,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) || strings.Contains(string(data), "export class ApiError") {
			t.Errorf("%s should import the shared runtime (%s) and not declare ApiError", path, want)
		}
	}
	if _, err := exec.LookPath("tsc"); err == nil {
		writeTestFile(t, filepath.Join(out, "tsconfig.json"), `{"compilerOptions": {"target": "ES2022", "module": "ES2022", "moduleResolution": "bundler", "strict": true, "noUnusedLocals": true, "noUnusedParameters": true, "noEmit": true, "lib": ["ES2022", "DOM"], "types": []}}`)
		cmd := exec.Command("tsc", "-p", "tsconfig.json")
		cmd.Dir = out
		if combined, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the built tree must type-check under the strict flags: %v\n%s", err, combined)
		}
	}
}

func TestTSSharedRuntimeHonorsRuntimeDirAndCleansUpWhenSwitchedBack(t *testing.T) {
	dir := writeTSSharedProject(t, "runtime = \"shared\"\nruntime_dir = \"core/rt\"\n")
	if err := Build(dir); err != nil {
		t.Fatalf("Build: %v", err)
	}
	runtime := filepath.Join(dir, "web", "api", "core", "rt", "runtime.ts")
	if _, err := os.Stat(runtime); err != nil {
		t.Fatalf("runtime_dir was not honored: %v", err)
	}
	client, _ := os.ReadFile(filepath.Join(dir, "web", "api", "orders", "client.ts"))
	if !strings.Contains(string(client), `from "../core/rt/runtime.js"`) {
		t.Errorf("the client should import from the configured directory:\n%.300s", client)
	}
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/app\"\n\n[generate.ts-client]\nout = \"web/api\"\n")
	if err := Build(dir); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if _, err := os.Stat(runtime); !os.IsNotExist(err) {
		t.Fatalf("the shared runtime should be removed when the project goes back to per-package runtimes: %v", err)
	}
	client, _ = os.ReadFile(filepath.Join(dir, "web", "api", "orders", "client.ts"))
	if !strings.Contains(string(client), "export class ApiError") {
		t.Errorf("each client declares its own runtime again")
	}
}

func TestTSRuntimeConfigValidation(t *testing.T) {
	for name, tc := range map[string]struct{ toml, want string }{
		"unknown mode":    {"runtime = \"global\"\n", "ts-client runtime must be"},
		"dir needs mode":  {"runtime_dir = \"rt\"\n", "needs runtime"},
		"dir outside out": {"runtime = \"shared\"\nruntime_dir = \"../rt\"\n", "inside the output path"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeTSSharedProject(t, tc.toml)
			err := Build(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	t.Run("collision with a schema directory", func(t *testing.T) {
		dir := writeTSSharedProject(t, "runtime = \"shared\"\nruntime_dir = \"orders\"\n")
		err := Build(dir)
		if err == nil || !strings.Contains(err.Error(), "collides with the schema directory") {
			t.Fatalf("error = %v", err)
		}
	})
}
