package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func readAppendFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "appendjson", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeTreeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, content)
}

func generateAppendFixture(t *testing.T, schema string, emitZero bool) string {
	t.Helper()
	ast, err := onklang.Parse(schema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: emitZero})
	if err != nil {
		t.Fatal(err)
	}
	out, err := GenerateTypesWithResolver(pkg.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestMessagesGetAnAppendEncoder(t *testing.T) {
	out := generateAppendFixture(t, readAppendFixture(t, "kitchen.onk"), true)
	for _, want := range []string{
		"func (m *Kitchen) AppendJSON(b []byte) ([]byte, error)",
		"func (m *Kitchen) MarshalJSON() ([]byte, error) {\n\treturn onkMarshalJSON(m)",
		"func (m *Kitchen) MarshalJSONTo(enc *jsontext.Encoder) error {\n\treturn onkMarshalJSONTo(enc, m)",
		"func (m *Holder) AppendJSON(b []byte) ([]byte, error)",
		"func (m *Tree) AppendJSON(b []byte) ([]byte, error)",
		"func (m *Leaf) AppendJSON(b []byte) ([]byte, error)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated types lack %q", want)
		}
	}
	if strings.Contains(out, "marshalAux") {
		t.Error("an encoder still builds a reflection aux struct")
	}
	if strings.Contains(out, "func (m *Leaf) MarshalJSON") {
		t.Error("a plain message reached from an appendable one gained a MarshalJSON, which would slow json.Marshal of it alone")
	}
	if strings.Contains(out, "func (m *Plain) AppendJSON") {
		t.Error("a message with no custom JSON anywhere below it was rewritten")
	}
}

func TestFlattenedMessagesKeepTheReflectionEncoder(t *testing.T) {
	out := generateAppendFixture(t, `package app

message Inner { v: string }

message Outer {
  name: string
  inner: Inner @flatten(prefix: "in_")
}
`, false)
	if strings.Contains(out, "AppendJSON") {
		t.Fatalf("a flattened message was given an append encoder:\n%s", out)
	}
}

func TestServerWritesResponsesThroughTheAppendEncoder(t *testing.T) {
	file := compileFixtureSource(t, `package app

message Req { id: string }
message Res { id: string  tags: string[] }

service S {
  get(Req) -> Res @post("/x")
}
`)
	server, err := GenerateServerWithOptions(file, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(server), "value.(interface{ AppendJSON([]byte) ([]byte, error) })") {
		t.Fatalf("generated server does not use the append encoder:\n%s", server)
	}
}

func TestAppendEncoderMatchesTheReflectionEncoder(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	schema := readAppendFixture(t, "kitchen.onk")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/diff\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "fill_test.go"), readAppendFixture(t, "fill_test.go.txt"))
	for i, emitZero := range []bool{false, true} {
		n := strconv.Itoa(i)
		writeTreeFile(t, filepath.Join(dir, "legacy"+n, "types.go"), readAppendFixture(t, "legacy_ez"+n+".go.txt"))
		writeTreeFile(t, filepath.Join(dir, "fresh"+n, "types.go"), generateAppendFixture(t, schema, emitZero))
		pair := strings.ReplaceAll(readAppendFixture(t, "pair_test.go.txt"), "FRESHDIR", "fresh"+n)
		pair = strings.ReplaceAll(pair, "LEGACYDIR", "legacy"+n)
		pair = strings.ReplaceAll(pair, "func freshVariants", "func freshVariants"+n)
		pair = strings.ReplaceAll(pair, "func legacyVariants", "func legacyVariants"+n)
		pair = strings.ReplaceAll(pair, "freshVariants()", "freshVariants"+n+"()")
		pair = strings.ReplaceAll(pair, "legacyVariants()", "legacyVariants"+n+"()")
		pair = strings.ReplaceAll(pair, "type appender interface", "type appender"+n+" interface")
		pair = strings.ReplaceAll(pair, "a.(appender)", "a.(appender"+n+")")
		pair = strings.ReplaceAll(pair, "c.fresh.(appender)", "c.fresh.(appender"+n+")")
		for _, name := range []string{"compare", "TestMatchesLegacy", "TestEncodeErrorsMatchLegacy", "TestNilAndEmptyStatesMatchLegacy", "TestNilReceiverEncodesNull"} {
			pair = strings.ReplaceAll(pair, name+"(", name+n+"(")
		}
		writeFile(t, filepath.Join(dir, "pair"+n+"_test.go"), pair)
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("\n%s", out)
}

var benchLine = regexp.MustCompile(`(?m)^BenchmarkEncode/(n=\d+)/(\S+?)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op`)

func TestAppendEncoderBenchmarkAgainstThePlainStruct(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmarks run in the full suite only")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/bench\n\ngo 1.27\n")
	writeTreeFile(t, filepath.Join(dir, "legacy", "types.go"), readAppendFixture(t, "resource_legacy.go.txt"))
	writeTreeFile(t, filepath.Join(dir, "fresh", "types.go"), generateAppendFixture(t, readAppendFixture(t, "resource.onk"), true))
	writeFile(t, filepath.Join(dir, "bench_test.go"), readAppendFixture(t, "bench_test.go.txt"))
	cmd := exec.Command("go", "test", "-run", "XXX", "-bench", ".", "-benchmem", "-benchtime", "3000x", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("\n%s", out)
	allocs := map[string]int{}
	for _, m := range benchLine.FindAllStringSubmatch(string(out), -1) {
		n, _ := strconv.Atoi(m[5])
		allocs[m[1]+"/"+m[2]] = n
	}
	for _, size := range []string{"n=10", "n=30"} {
		before, after, plain := allocs[size+"/before-json.Marshal"], allocs[size+"/after-json.Marshal"], allocs[size+"/plain-struct-json.Marshal"]
		if before == 0 || after == 0 || plain == 0 {
			t.Fatalf("%s: missing benchmark results in\n%s", size, out)
		}
		if after*3 > before {
			t.Errorf("%s: json.Marshal allocates %d times, was %d: the encoder is not one pass", size, after, before)
		}
		if after > plain {
			t.Errorf("%s: json.Marshal allocates %d times, a plain struct %d", size, after, plain)
		}
	}
}
