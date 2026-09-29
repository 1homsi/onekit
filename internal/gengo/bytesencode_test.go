package gengo

import (
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

// bytesEncodeFixture exercises every @encode value the validator allows on a
// bytes field, in both required and optional cardinality, alongside an
// unannotated bytes field that must keep encoding/json's stdlib base64.
const bytesEncodeFixture = `
package main

message Blobs {
  plain: bytes
  plain_opt: bytes?
  std: bytes @encode(base64)
  std_opt: bytes? @encode(base64)
  raw: bytes @encode(base64_raw)
  raw_opt: bytes? @encode(base64_raw)
  url: bytes @encode(base64url)
  url_opt: bytes? @encode(base64url)
  url_raw: bytes @encode(base64url_raw)
  url_raw_opt: bytes? @encode(base64url_raw)
  hexed: bytes @encode(hex)
  hex_opt: bytes? @encode(hex)
}
`

// allowedBytesEncodings mirrors the bytes branch of validateCompiledField in
// internal/onkcompile/validate.go. The validator's own set is unexported, so
// the test restates it and cross-checks the two stay aligned.
var allowedBytesEncodings = map[string]bool{
	"hex": true, "base64": true, "base64_raw": true,
	"base64url": true, "base64url_raw": true,
}

// bytesEncodeHarness recomputes the expected wire strings with the standard
// library and compares them against what the generated types emit, so a
// double-encode or a wrong base64 alphabet is caught without hardcoding.
const bytesEncodeHarness = `
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		fail("quote %q: %v", s, err)
	}
	return string(b)
}

// fieldJSON returns the wire string for one field so a wrong alphabet or
// padding shows up as a diff instead of a silent round-trip through our own
// decoder.
func fieldJSON(raw []byte, name string) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		fail("re-decode %s: %v", name, err)
	}
	v, ok := m[name]
	if !ok {
		fail("field %s missing from %s", name, string(raw))
	}
	s, ok := v.(string)
	if !ok {
		fail("field %s is %T, want string (got %s)", name, v, string(raw))
	}
	return s
}

func main() {
	// Bytes whose standard base64 uses '+' and '/' so a URL-alphabet mixup or
	// a raw-vs-padded mixup is observable, plus a non-ASCII tail.
	payload := []byte{0xfb, 0xff, 0x00, 0x10, 'h', 'i', 0xfe}

	std := base64.StdEncoding.EncodeToString(payload)
	rawStd := base64.RawStdEncoding.EncodeToString(payload)
	urlStd := base64.URLEncoding.EncodeToString(payload)
	urlRaw := base64.RawURLEncoding.EncodeToString(payload)
	hexStr := hex.EncodeToString(payload)

	opt := payload
	m := &Blobs{
		Plain:       payload,
		PlainOpt:    &opt,
		Std:         payload,
		StdOpt:      &opt,
		Raw:         payload,
		RawOpt:      &opt,
		Url:         payload,
		UrlOpt:      &opt,
		UrlRaw:      payload,
		UrlRawOpt:   &opt,
		Hexed:       payload,
		HexOpt:      &opt,
	}

	b, err := json.Marshal(m)
	if err != nil {
		fail("marshal Blobs: %v", err)
	}
	s := string(b)

	// Unannotated bytes keeps encoding/json's stdlib base64 (with padding).
	if got := fieldJSON(b, "plain"); got != std {
		fail("plain: got %q want %q (%s)", got, std, s)
	}
	if got := fieldJSON(b, "plain_opt"); got != std {
		fail("plain_opt: got %q want %q (%s)", got, std, s)
	}

	// @encode(base64) is padded standard base64, matching the default.
	if got := fieldJSON(b, "std"); got != std {
		fail("std: got %q want %q (%s)", got, std, s)
	}
	if got := fieldJSON(b, "std_opt"); got != std {
		fail("std_opt: got %q want %q (%s)", got, std, s)
	}

	// Non-regression controls for the encodings that already worked.
	for _, tc := range []struct{ name, want string }{
		{"raw", rawStd},
		{"raw_opt", rawStd},
		{"url", urlStd},
		{"url_opt", urlStd},
		{"url_raw", urlRaw},
		{"url_raw_opt", urlRaw},
		{"hexed", hexStr},
		{"hex_opt", hexStr},
	} {
		if got := fieldJSON(b, tc.name); got != tc.want {
			fail("%s: got %q want %q (%s)", tc.name, got, tc.want, s)
		}
	}

	// Round-trip every field back through the generated UnmarshalJSON.
	var back Blobs
	if err := json.Unmarshal(b, &back); err != nil {
		fail("unmarshal Blobs: %v", err)
	}
	check := func(name string, got []byte) {
		if string(got) != string(payload) {
			fail("%s round trip: got %x want %x", name, got, payload)
		}
	}
	check("plain", back.Plain)
	check("std", back.Std)
	check("raw", back.Raw)
	check("url", back.Url)
	check("url_raw", back.UrlRaw)
	check("hexed", back.Hexed)
	if back.PlainOpt == nil || back.StdOpt == nil || back.RawOpt == nil ||
		back.UrlOpt == nil || back.UrlRawOpt == nil || back.HexOpt == nil {
		fail("optional fields lost on round trip: %+v", back)
	}
	check("plain_opt", *back.PlainOpt)
	check("std_opt", *back.StdOpt)
	check("raw_opt", *back.RawOpt)
	check("url_opt", *back.UrlOpt)
	check("url_raw_opt", *back.UrlRawOpt)
	check("hex_opt", *back.HexOpt)

	// An absent field must not be turned into an empty non-nil slice.
	var sparse Blobs
	if err := json.Unmarshal([]byte("{}"), &sparse); err != nil {
		fail("unmarshal Blobs({}): %v", err)
	}
	if sparse.Std != nil || sparse.StdOpt != nil {
		fail("absent base64 field decoded to %+v", sparse)
	}

	// @encode(base64) must decode what encoding/json would have produced, so
	// the default wire form stays interchangeable with the annotated one.
	var interop Blobs
	if err := json.Unmarshal([]byte("{\"std\":"+quote(std)+"}"), &interop); err != nil {
		fail("unmarshal plain base64 into std: %v", err)
	}
	check("std (interop)", interop.Std)

	// A value that is not valid base64 must be reported, not silently
	// truncated to a shorter byte slice.
	var bad Blobs
	if err := json.Unmarshal([]byte("{\"std\":\"!!!!\"}"), &bad); err == nil {
		fail("expected error decoding invalid base64, got %+v", bad)
	}

	fmt.Println("OK")
}
`

// generateBytesEncodeTypes compiles the fixture and runs the Go type
// generator over it, failing the test on any compile error.
func generateBytesEncodeTypes(t *testing.T) string {
	t.Helper()
	ast, err := onklang.Parse(bytesEncodeFixture)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	src, err := GenerateTypes(pkg.Files[0])
	if err != nil {
		t.Fatalf("GenerateTypes error: %v\n%s", err, src)
	}
	return string(src)
}

// TestBytesEncodeAllFormsAreValidGo guards the regression where
// @encode(base64) on a bytes field produced a custom MarshalJSON/UnmarshalJSON
// pair that did not compile: the raw []byte expression was assigned into a
// string aux field, and the decode side emitted `decoded, err := aux.Data`.
func TestBytesEncodeAllFormsAreValidGo(t *testing.T) {
	src := generateBytesEncodeTypes(t)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "types.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated types do not parse: %v\n%s", err, src)
	}

	// The custom marshal path is emitted for every annotated field, so the
	// base64 import must come with it.
	if !strings.Contains(src, `"encoding/base64"`) {
		t.Errorf("expected encoding/base64 import, got:\n%s", src)
	}
	if !strings.Contains(src, "base64.StdEncoding.EncodeToString(") {
		t.Errorf("expected base64.StdEncoding.EncodeToString call, got:\n%s", src)
	}
	if !strings.Contains(src, "base64.StdEncoding.DecodeString(") {
		t.Errorf("expected base64.StdEncoding.DecodeString call, got:\n%s", src)
	}

	// Every annotated bytes field must decode through a real two-value call.
	// The regression emitted `decoded, err := aux.Data` (one value, two
	// targets), which parses cleanly and only fails the type check, so pin the
	// shape here too.
	if strings.Contains(src, "decoded, err := aux.") {
		t.Errorf("bare `decoded, err := aux.X` is a one-value assignment, got:\n%s", src)
	}
	// The aux field is a string, so the marshal side must always wrap the
	// []byte in an encoder call rather than assigning it directly.
	if strings.Contains(src, "aux.Std = m.Std\n") {
		t.Errorf("raw []byte assigned into string aux field, got:\n%s", src)
	}
}

// TestBytesEncodeCallCoversAllowlist pins the encoder/decoder dispatch to the
// exact set of @encode values onkcompile accepts for a bytes field. The
// regression was an allowlisted value ("base64") with no case in these two
// switches, so its default branch emitted a bare []byte expression. Keep the
// literal lists in sync with validateCompiledField in
// internal/onkcompile/validate.go.
func TestBytesEncodeCallCoversAllowlist(t *testing.T) {
	cases := []struct {
		encoding string
		encode   string
		decode   string
	}{
		{bytesEncodeHex, "hex.EncodeToString(V)", "hex.DecodeString(V)"},
		{bytesEncodeBase64, "base64.StdEncoding.EncodeToString(V)", "base64.StdEncoding.DecodeString(V)"},
		{bytesEncodeBase64Raw, "base64.RawStdEncoding.EncodeToString(V)", "base64.RawStdEncoding.DecodeString(V)"},
		{bytesEncodeBase64URL, "base64.URLEncoding.EncodeToString(V)", "base64.URLEncoding.DecodeString(V)"},
		{bytesEncodeBase64URLRaw, "base64.RawURLEncoding.EncodeToString(V)", "base64.RawURLEncoding.DecodeString(V)"},
	}
	if len(cases) != len(allowedBytesEncodings) {
		t.Errorf("table has %d encodings, allowlist has %d: %v",
			len(cases), len(allowedBytesEncodings), allowedBytesEncodings)
	}
	for _, tc := range cases {
		if !allowedBytesEncodings[tc.encoding] {
			t.Errorf("%q is not an allowlisted bytes encoding", tc.encoding)
		}
		if got := bytesEncodeCall(tc.encoding, "V"); got != tc.encode {
			t.Errorf("bytesEncodeCall(%q) = %q, want %q", tc.encoding, got, tc.encode)
		}
		if got := bytesDecodeCall(tc.encoding, "V"); got != tc.decode {
			t.Errorf("bytesDecodeCall(%q) = %q, want %q", tc.encoding, got, tc.decode)
		}
	}

	// An unannotated field must not reach these functions at all
	// (bytesEncodingValue returns ""), and "" must stay a no-op.
	if got := bytesEncodeCall("", "V"); got != "V" {
		t.Errorf(`bytesEncodeCall("") = %q, want "V"`, got)
	}
	if got := bytesDecodeCall("", "V"); got != "V" {
		t.Errorf(`bytesDecodeCall("") = %q, want "V"`, got)
	}
}

// TestBytesEncodeRoundTrip compiles and runs the generated types so the
// []byte -> wire string -> []byte path is checked at runtime, not just
// through the parser.
func TestBytesEncodeRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	src := generateBytesEncodeTypes(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_bytes_encode_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), src)
	writeFile(t, filepath.Join(dir, "main.go"), bytesEncodeHarness)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s\n--- generated types ---\n%s", err, out, src)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q\n--- generated types ---\n%s", got, src)
	}
}

// TestBytesEncodeNoAnnotationUsesStdlib pins the default case: a bytes field
// with no @encode must not get a custom marshaler, must not be double
// encoded, and must be covered by the stdlib import decision.
func TestBytesEncodeNoAnnotationUsesStdlib(t *testing.T) {
	ast, err := onklang.Parse(`
package main

message Plain {
  data: bytes
}
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	if fileNeedsJSONHelpers(file) {
		t.Error("plain bytes field should not require custom JSON helpers")
	}
	if imp := fileNeedsEncodingImports(file); imp.base64 || imp.hex {
		t.Errorf("plain bytes field should not pull encoding imports, got %+v", imp)
	}

	src, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v\n%s", err, src)
	}
	got := string(src)
	if strings.Contains(got, "MarshalJSON") {
		t.Errorf("plain bytes field should not emit a custom MarshalJSON, got:\n%s", got)
	}
	if strings.Contains(got, "encoding/base64") {
		t.Errorf("plain bytes field should not import encoding/base64, got:\n%s", got)
	}
	if !strings.Contains(got, "Data []byte `json:\"data,omitempty\"`") {
		t.Errorf("expected plain []byte field, got:\n%s", got)
	}
}

// TestClientBodyUsesBase64ForAnnotatedBytes covers the second emitter of
// bytesEncodeCall: the generated client wraps the request body in a wire
// value expression, so it needs the encoding/base64 import too.
func TestClientBodyUsesBase64ForAnnotatedBytes(t *testing.T) {
	ast, err := onklang.Parse(`
package main

message Req {
  body: bytes @encode(base64)
}

message Ack {
  ok: bool
}

service Files {
  base_path: "/v1"

  upload(Req) -> Ack @post("/upload") @body("body")
}
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	src, err := GenerateClient(pkg.Files[0])
	if err != nil {
		t.Fatalf("GenerateClient error: %v\n%s", err, src)
	}
	got := string(src)
	if !strings.Contains(got, `"encoding/base64"`) {
		t.Errorf("expected encoding/base64 import in client, got:\n%s", got)
	}
	if !strings.Contains(got, "base64.StdEncoding.EncodeToString(") {
		t.Errorf("expected base64.StdEncoding.EncodeToString in client body, got:\n%s", got)
	}
}
