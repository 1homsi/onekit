package onek

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const incrementalConfig = `module = "example.com/inc/gen"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts"

[generate.ts-server]
out = "./gen/tss"

[generate.python-client]
out = "./gen/py"

[generate.dart-client]
out = "./gen/dart"

[generate.openapi]
out = "./gen/openapi"
`

var incrementalSchemas = map[string]string{
	"common/v1/models.onk": `package common.v1

enum Level {
  LOW

  HIGH
}

message Money {
  amount_cents: int64

  currency: string
}

message User {
  id: string

  level: Level
}
`,
	"billing/v1/api.onk": `package billing.v1

import "../../common/v1/models.onk"

message Invoice {
  id: string

  total: common.v1.Money
}

message GetInvoice {
  id: string
}

service Billing {
  get(GetInvoice) -> Invoice @get("/invoices/{id}")
}
`,
	"hub/v1/api.onk": `package hub.v1

import "../../common/v1/models.onk"

message Profile {
  user: common.v1.User

  bio: string
}

message GetProfile {
  id: string
}

service Hub {
  get(GetProfile) -> Profile @get("/profiles/{id}")
  watch(GetProfile) -> Profile @get("/profiles/{id}/watch") @stream
}
`,
	"leaf/v1/api.onk": `package leaf.v1

message Leaf {
  id: string

  size: int32
}

service Leaves {
  get(Leaf) -> Leaf @get("/leaves/{id}")
}
`,
}

func writeIncrementalProject(t *testing.T, dir string, config string, schemas map[string]string) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), config)
	for rel, src := range schemas {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(rel)), src)
	}
}

func generatedTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	tree := map[string][]byte{}
	for _, root := range []string{"gen", ".onekit"} {
		base := filepath.Join(dir, root)
		_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, _ := filepath.Rel(dir, path)
			tree[filepath.ToSlash(rel)] = data
			return nil
		})
	}
	return tree
}

func compareTrees(t *testing.T, step string, got, want map[string][]byte) {
	t.Helper()
	var names []string
	for name := range want {
		names = append(names, name)
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: incremental build has an extra file %s", step, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, ok := got[name]
		if !ok {
			t.Errorf("%s: incremental build is missing %s", step, name)
			continue
		}
		if !bytes.Equal(data, want[name]) {
			t.Errorf("%s: %s differs from a full build", step, name)
		}
	}
}

func TestIncrementalBuildMatchesAFullBuild(t *testing.T) {
	t.Setenv(cacheDirEnv, t.TempDir())
	inc := t.TempDir()
	config := incrementalConfig
	schemas := map[string]string{}
	for rel, src := range incrementalSchemas {
		schemas[rel] = src
	}
	writeIncrementalProject(t, inc, config, schemas)

	groups := func() int64 { return int64(len(schemas)) }
	step := func(name string, wantClean int64) {
		t.Helper()
		if err := Build(inc); err != nil {
			t.Fatalf("%s: incremental build: %v", name, err)
		}
		if wantClean >= 0 && lastCleanGroups.Load() != wantClean {
			t.Errorf("%s: %d directories kept, want %d", name, lastCleanGroups.Load(), wantClean)
		}
		ref := t.TempDir()
		writeIncrementalProject(t, ref, config, schemas)
		t.Setenv(noCacheEnv, "1")
		err := Build(ref)
		t.Setenv(noCacheEnv, "")
		if err != nil {
			t.Fatalf("%s: full build: %v", name, err)
		}
		compareTrees(t, name, generatedTree(t, inc), generatedTree(t, ref))
	}
	edit := func(rel, old, replacement string) {
		t.Helper()
		if !strings.Contains(schemas[rel], old) {
			t.Fatalf("%s has no %q", rel, old)
		}
		schemas[rel] = strings.Replace(schemas[rel], old, replacement, 1)
		writeTestFile(t, filepath.Join(inc, filepath.FromSlash(rel)), schemas[rel])
	}

	step("first build", 0)
	edit("leaf/v1/api.onk", "size: int32", "size: int32\n\n  color: string")
	step("a leaf package changed", groups()-1)
	edit("common/v1/models.onk", "currency: string", "currency: string\n\n  rate: float64")
	step("a shared package changed", groups()-3)
	edit("common/v1/models.onk", "level: Level", "level: Level\n\n  tags: string[]")
	step("a type a dependent uses changed", groups()-3)
	step("nothing changed", -1)

	schemas["extra/v1/api.onk"] = "package extra.v1\n\nmessage Extra {\n  id: string\n}\n"
	writeTestFile(t, filepath.Join(inc, "extra", "v1", "api.onk"), schemas["extra/v1/api.onk"])
	step("a package was added", 0)

	delete(schemas, "leaf/v1/api.onk")
	if err := os.RemoveAll(filepath.Join(inc, "leaf")); err != nil {
		t.Fatal(err)
	}
	step("a package was removed", 0)

	if err := os.Remove(filepath.Join(inc, "gen", "go", "billing", "v1", "types.gen.go")); err != nil {
		t.Fatal(err)
	}
	edit("extra/v1/api.onk", "id: string", "id: string\n\n  n: int32")
	step("a generated file was deleted by hand", groups()-2)

	if err := os.WriteFile(filepath.Join(inc, "gen", "ts", "hub", "v1", "types.ts"), []byte("// Code generated by onkt. DO NOT EDIT.\n// edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit("extra/v1/api.onk", "n: int32", "n: int64")
	step("a generated file was edited by hand", groups()-2)

	config = strings.Replace(config, `module = "example.com/inc/gen"`, "module = \"example.com/inc/gen\"\nroute_prefix = \"/api\"", 1)
	writeTestFile(t, filepath.Join(inc, "onekit.toml"), config)
	step("the config changed", 0)
}

func TestCleanupNeverDeletesThroughASymlinkedOutputRoot(t *testing.T) {
	t.Setenv(cacheDirEnv, t.TempDir())
	project := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(project, "onekit.toml"), "module = \"example.com/s/gen\"\n\n[generate.go-server]\nout = \"./gen\"\n")
	writeTestFile(t, filepath.Join(project, "api", "api.onk"), "package api\n\nmessage A {\n  id: string\n}\n")
	if err := Build(project); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "api", "types.gen.go")
	writeTestFile(t, victim, "// Code generated by onkt. DO NOT EDIT.\npackage api\n")
	if err := os.RemoveAll(filepath.Join(project, "gen")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "gen")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	writeTestFile(t, filepath.Join(project, "onekit.toml"), "module = \"example.com/s/gen\"\n\n[generate.go-server]\nout = \"./gen2\"\n")
	if err := Build(project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("cleanup of the old output root deleted a file through its symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "gen2", "api", "types.gen.go")); err != nil {
		t.Fatalf("the new output root must be generated: %v", err)
	}
}
