package playground

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sampleSchema = `package example.todos

enum Status { OPEN DONE }

message Todo {
  id: string
  title: string @required @len(1, 200)
  status: Status
}

message GetTodo { id: string @required }
message NotFound @status(404) { message: string }

service Todos {
  base_path: "/v1"

  getTodo(GetTodo) -> Todo | NotFound @get("/todos/{id}")
}
`

func TestRunGeneratesEveryTarget(t *testing.T) {
	out := Run(sampleSchema)
	if len(out.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", out.Diagnostics)
	}
	got := map[string]bool{}
	for _, f := range out.Files {
		got[f.Target+"/"+f.Name] = true
		if strings.TrimSpace(f.Content) == "" {
			t.Errorf("%s/%s is empty", f.Target, f.Name)
		}
	}
	for _, want := range []string{
		"go/types.gen.go", "go/validate.gen.go", "go/server.gen.go", "go/client.gen.go",
		"typescript/types.ts", "typescript/client.ts", "typescript/server.ts",
		"python/models.py", "python/client.py",
		"dart/models.dart", "dart/client.dart",
		"rust/types.rs", "rust/server.rs", "rust/client.rs",
		"openapi/openapi.yaml",
	} {
		if !got[want] {
			t.Errorf("missing generated file %s (have %v)", want, got)
		}
	}
	var openapi string
	for _, f := range out.Files {
		if f.Target == "openapi" {
			openapi = f.Content
		}
	}
	if !strings.Contains(openapi, "/v1/todos/{id}") {
		t.Fatalf("openapi does not describe the route:\n%s", openapi)
	}
}

func TestRunReportsParseErrorsWithPositions(t *testing.T) {
	out := Run("package p\nmessage M {\n  id string\n}\n")
	if len(out.Files) != 0 || len(out.Diagnostics) != 1 {
		t.Fatalf("output = %+v", out)
	}
	d := out.Diagnostics[0]
	if d.Code != "parse_error" || d.Line != 3 || d.Message == "" {
		t.Fatalf("diagnostic = %+v", d)
	}
}

func TestRunReportsCompileErrors(t *testing.T) {
	out := Run("package p\nmessage M { id: string }\nmessage M { id: string }\n")
	if len(out.Files) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Message == "" {
		t.Fatalf("output = %+v", out)
	}
}

func TestRunRejectsOversizedSchemas(t *testing.T) {
	out := Run(strings.Repeat("// padding\n", MaxSchemaBytes/10))
	if len(out.Files) != 0 || len(out.Diagnostics) != 1 || !strings.Contains(out.Diagnostics[0].Message, "playground limit") {
		t.Fatalf("output = %+v", out.Diagnostics)
	}
}

func TestRunNeverReturnsNilSlices(t *testing.T) {
	data, err := json.Marshal(Run("not a schema {"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"files":[]`) {
		t.Fatalf("the page iterates files, so an empty list must encode as []: %s", data)
	}
}

func TestFormatCanonicalizesAndReportsErrors(t *testing.T) {
	formatted, problem := Format("package p\nmessage   M{id:string}\n")
	if problem != nil || !strings.Contains(formatted, "message M {") || !strings.Contains(formatted, "id: string") {
		t.Fatalf("formatted = %q, problem = %+v", formatted, problem)
	}
	if _, problem := Format("package p\nmessage"); problem == nil {
		t.Fatal("a broken schema must produce a diagnostic")
	}
}

func TestWebAssemblyBundleMatchesTheNativeResult(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "onek.wasm"), "./playground/wasm")
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the wasm bundle: %v\n%s", err, out)
	}
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	execJS := ""
	for _, candidate := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		path := filepath.Join(strings.TrimSpace(string(root)), filepath.FromSlash(candidate))
		if _, statErr := os.Stat(path); statErr == nil {
			execJS = path
		}
	}
	if execJS == "" {
		t.Skip("wasm_exec.js not found in GOROOT")
	}
	runner := `const fs = require("fs");
require(process.argv[2]);
const go = new Go();
WebAssembly.instantiate(fs.readFileSync(process.argv[3]), go.importObject).then((result) => {
  go.run(result.instance);
  const schema = fs.readFileSync(process.argv[4], "utf8");
  fs.writeFileSync(process.argv[5], JSON.stringify({ generated: JSON.parse(globalThis.onekGenerate(schema)), formatted: JSON.parse(globalThis.onekFormat("package p\nmessage   M{id:string}\n")) }));
  process.exit(0);
});
`
	schemaPath := filepath.Join(dir, "schema.onk")
	for name, content := range map[string]string{"run.js": runner, "schema.onk": sampleSchema} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resultPath := filepath.Join(dir, "result.json")
	cmd := exec.Command("node", filepath.Join(dir, "run.js"), execJS, filepath.Join(dir, "onek.wasm"), schemaPath, resultPath)
	if combined, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("running the bundle in node: %v\n%s", runErr, combined)
	}
	out, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var fromWasm struct {
		Generated Output `json:"generated"`
		Formatted struct {
			Formatted string `json:"formatted"`
		} `json:"formatted"`
	}
	if err := json.Unmarshal(out, &fromWasm); err != nil {
		t.Fatalf("bundle output is not JSON: %v\n%s", err, out)
	}
	native, err := json.Marshal(Run(sampleSchema))
	if err != nil {
		t.Fatal(err)
	}
	inWasm, err := json.Marshal(fromWasm.Generated)
	if err != nil {
		t.Fatal(err)
	}
	if string(native) != string(inWasm) {
		t.Fatalf("the wasm bundle generated different output than the native build (%d vs %d bytes)", len(inWasm), len(native))
	}
	if !strings.Contains(fromWasm.Formatted.Formatted, "message M {") {
		t.Fatalf("onekFormat = %q", fromWasm.Formatted.Formatted)
	}
}
