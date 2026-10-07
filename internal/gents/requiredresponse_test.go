package gents

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const requiredResponseSchema = `package app

enum Level { LOW HIGH }
message Inner { count: int32  label: string }
message Env {
  id: int64 @encode("number")
  is_default: bool
  name: string
  level: Level
  tags: string[]
  by_name: map[string, string]
  note: string?
  inner: Inner
  items: Inner[]
  created_at: timestamp
  parent: Env
  maybe_inner: Inner?
}
message Patch { id: int64 @encode("number")  is_default: bool  name: string }
message Shared { id: int32  flag: bool }
message Lookup { id: int64 @encode("number") }
message Gone @status(404) { reason: string }

service Envs {
  base_path: "/v1"
  get(Lookup) -> Env | Gone @get("/envs/{id}")
  update(Patch) -> Shared @post("/envs")
  echo(Shared) -> Shared @post("/echo")
}
`

func compileWithEmitZero(t *testing.T, emitZero bool) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(requiredResponseSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "api.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: emitZero})
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Files[0]
}

func TestTSResponseOnlyFieldsAreRequiredWhenZeroValuesAreAlwaysSent(t *testing.T) {
	types := string(GenerateTypes(compileWithEmitZero(t, true)))
	body := func(name string) string {
		start := strings.Index(types, "export interface "+name+" {")
		if start < 0 {
			t.Fatalf("no interface %s", name)
		}
		return types[start : start+strings.Index(types[start:], "\n}")]
	}
	env := body("Env")
	for _, want := range []string{"id: number;", "isDefault: boolean;", "name: string;", "level: Level;", "tags: (string)[];", "byName: Record<string, string>;", "inner: Inner;", "createdAt: string;"} {
		if !strings.Contains(env, want) {
			t.Errorf("a response-only field that is always sent must be required, missing %q in:\n%s", want, env)
		}
	}
	for _, want := range []string{"note?: string | undefined", "maybeInner?: Inner | undefined", "parent?: Env | undefined"} {
		if !strings.Contains(env, want) {
			t.Errorf("optional fields and self-referencing messages stay optional, missing %q in:\n%s", want, env)
		}
	}
	if !strings.Contains(body("Inner"), "count: number;") || !strings.Contains(body("Gone"), "reason: string;") {
		t.Errorf("messages nested in a response and declared errors count as response-only:\n%s\n%s", body("Inner"), body("Gone"))
	}
	if !strings.Contains(body("Patch"), "isDefault?: boolean | undefined;") || !strings.Contains(body("Lookup"), "id?: number | undefined;") {
		t.Errorf("request messages keep optional fields so callers can send partial objects:\n%s", types)
	}
	if !strings.Contains(body("Shared"), "flag?: boolean | undefined;") {
		t.Errorf("a message used as both request and response stays optional:\n%s", body("Shared"))
	}
}

func TestTSFieldsStayOptionalWithoutEmitZeroValues(t *testing.T) {
	types := string(GenerateTypes(compileWithEmitZero(t, false)))
	if !strings.Contains(types, "isDefault?: boolean | undefined;") || strings.Contains(types, "isDefault: boolean;") {
		t.Fatalf("without emit_zero_values the server may omit zero values, so fields stay optional:\n%s", types)
	}
}

func TestTSRequiredResponseFieldsTypeCheckAndRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	file := compileWithEmitZero(t, true)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(GenerateServer(file)))
	writeFile(t, filepath.Join(dir, "use.ts"), `
import type { Env, Patch } from "./types.ts";
import { decodeEnv } from "./types.ts";
const env: Env = decodeEnv({ id: 1, is_default: false, name: "", level: "LOW", tags: [], by_name: {}, items: [] });
const id: number = env.id;
const flag: boolean = env.isDefault;
const partial: Patch = { name: "only the name" };
export { id, flag, partial };
`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions": {"target": "ES2022", "module": "ES2022", "moduleResolution": "bundler", "strict": true, "noEmit": true, "lib": ["ES2022", "DOM"], "allowImportingTsExtensions": true}}`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}
