package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

// rustNonRawIdentifiers pins the four keywords Rust refuses to spell with the
// raw-identifier syntax. `r#self`, `r#Self`, `r#super` and `r#crate` are all
// hard parse errors ("`self` cannot be a raw identifier"), so they must be
// mangled with a trailing underscore instead.
//
// The names are hard-coded rather than derived from rustKeywords so this test
// fails loudly if a future Rust release changes the raw-identifier rule.
var rustNonRawIdentifiers = []string{"self", "Self", "super", "crate"}

// keywordFixture exercises every name shape that reaches the Rust identifier
// helpers: schema field names (snake_case path), declaration and enum value
// names (PascalCase path), and RPC method names.
const keywordFixture = `
package kwfix

message Flags {
  self: bool
  super: bool
  crate: bool
  type: bool
  ok: bool
}

message Upper {
  Self: bool
}

enum Mode {
  self
  super
  crate
  normal
}

message Req { v: bool }
message Res { w: bool }

service Svc {
  ok(Req) -> Res @post("/b")
}
`

func compileKeywordFixture(t *testing.T) *onkir.Package {
	t.Helper()
	ast, err := onklang.Parse(keywordFixture)
	if err != nil {
		t.Fatalf("parse keyword fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "kwfix.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile keyword fixture: %v", err)
	}
	return pkg
}

func TestRustIdentMungesNonRawKeywords(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		// self/Self/super/crate cannot use r#, so they take a suffix.
		{"self", "self_"},
		{"Self", "self_"},
		{"super", "super_"},
		{"crate", "crate_"},
		// Ordinary Rust keywords still use the raw-identifier form.
		{"type", "r#type"},
		{"fn", "r#fn"},
		{"match", "r#match"},
		// Untouched controls.
		{"ok", "ok"},
		{"user_id", "user_id"},
		{"", "_"},
		{"1st", "_1st"},
	} {
		if got := RustIdent(tt.in); got != tt.want {
			t.Errorf("RustIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPascalCaseMungesNonRawKeywords(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		// A type or variant named `self` PascalCases to the `Self` keyword.
		{"self", "Self_"},
		{"Self", "Self_"},
		// Untouched controls: only `Self` survives PascalCasing as a keyword.
		{"super", "Super"},
		{"crate", "Crate"},
		{"type", "Type"},
		{"ok", "Ok"},
		{"user_id", "UserId"},
	} {
		if got := PascalCase(tt.in); got != tt.want {
			t.Errorf("PascalCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestKeywordManglingIsIdempotent guards the manglers against the recursion and
// re-mangling hazards of a suffix scheme: a second pass must be a no-op, so
// the helpers never loop and never emit `self__`.
func TestKeywordManglingIsIdempotent(t *testing.T) {
	for _, name := range []string{"self", "Self", "super", "crate"} {
		ident := RustIdent(name)
		if again := RustIdent(ident); again != ident {
			t.Errorf("RustIdent is not idempotent for %q: %q -> %q", name, ident, again)
		}
		pascal := PascalCase(name)
		if again := PascalCase(pascal); again != pascal {
			t.Errorf("PascalCase is not idempotent for %q: %q -> %q", name, pascal, again)
		}
	}
	if got := RustIdent("self_"); got != "self_" {
		t.Errorf("RustIdent(%q) = %q, want %q (already-mangled input must pass through)", "self_", got, "self_")
	}
	if got := PascalCase("self_"); got != "Self_" {
		t.Errorf("PascalCase(%q) = %q, want %q (already-mangled input must pass through)", "self_", got, "Self_")
	}
}

// TestRustKeywordsAreAllRawEscapable is the regression guard for the original
// defect: every name in the r# keyword table must be spellable as a raw
// identifier. The four forbidden names belong in the suffix set instead.
func TestRustKeywordsAreAllRawEscapable(t *testing.T) {
	forbidden := map[string]bool{}
	for _, name := range rustNonRawIdentifiers {
		forbidden[name] = true
	}
	for keyword := range rustKeywords {
		if forbidden[keyword] {
			t.Errorf("keyword %q cannot be used as a raw identifier and must not be in rustKeywords", keyword)
		}
	}
}

// TestGeneratedRustTypesMungeNonRawKeywords pins the emitted types: the four
// non-raw keywords must be suffixed, ordinary keywords must still be r#-escaped,
// and every wire name must stay unchanged via its serde rename.
func TestGeneratedRustTypesMungeNonRawKeywords(t *testing.T) {
	text := string(GenerateTypes(compileKeywordFixture(t).Files[0]))
	for _, want := range []string{
		"pub self_: bool,",
		"pub super_: bool,",
		"pub crate_: bool,",
		`#[serde(default, rename = "self")]`,
		`#[serde(default, rename = "super")]`,
		`#[serde(default, rename = "crate")]`,
		`#[serde(default, rename = "Self")]`,
		// `type` is raw-escapable and must stay that way.
		"pub r#type: bool,",
		// Untouched control field.
		"pub ok: bool,",
		// Enum variants: `self`/`super`/`crate` must be suffixed, not r#-escaped.
		`#[serde(rename = "self")]` + "\n    Self_,",
		// `super`/`crate` PascalCase to `Super`/`Crate`, which are legal Rust
		// identifiers - only `self` becomes the keyword `Self`.
		`#[serde(rename = "super")]` + "\n    Super,",
		`#[serde(rename = "crate")]` + "\n    Crate,",
		"fn default() -> Self { Self::Self_ }",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Rust types missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{
		"r#self", "r#super", "r#crate", "r#Self",
		"pub struct Self {", "pub struct Self:",
	} {
		if strings.Contains(text, unwanted) {
			t.Errorf("generated Rust types must not contain %q:\n%s", unwanted, text)
		}
	}
}

// TestGeneratedRustServiceMungeNonRawKeywords covers the service side, where a
// lowercase `self` service name would otherwise emit `pub trait Self` and
// `<T: Self>`. The compiler now rejects lowercase declaration names, so the
// assertion is a regression guard on the emitted trait spelling.
func TestGeneratedRustServiceMungeNonRawKeywords(t *testing.T) {
	text := string(GenerateServer(compileKeywordFixture(t).Files[0]))
	for _, want := range []string{
		"pub trait Svc: Send + Sync + 'static {",
		"pub fn svc_router<T: Svc>(service: Arc<T>) -> Router {",
		"fn ok(&self, context: RequestContext, req: Req)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Rust server missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"trait Self:", "<T: Self>", "r#self", "r#super", "r#crate"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("generated Rust server must not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestGeneratedRustClientMungesNonRawKeywords(t *testing.T) {
	text := string(GenerateClient(compileKeywordFixture(t).Files[0]))
	for _, want := range []string{
		"pub struct SvcClient {",
		"pub async fn ok(&self, req: &Req) -> Result<Res, SvcOkError> {",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Rust client missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "r#self") {
		t.Errorf("generated Rust client must not contain r#self:\n%s", text)
	}
}

// TestNonRawKeywordSuffixDoesNotCollide documents why suffixing is safe: the
// schema compiler's existing "collides after target-language name conversion"
// check already rejects the only inputs whose mangled forms could clash.
func TestNonRawKeywordSuffixDoesNotCollide(t *testing.T) {
	for _, src := range []string{
		"package collide\nmessage M { self: bool  self_: bool }",
		"package collide\nenum E { self  self_ }",
	} {
		ast, err := onklang.Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if _, err := onkcompile.Compile([]onkcompile.Source{{Path: "collide.onk", AST: ast}}); err == nil {
			t.Errorf("expected %q to be rejected as a generated-name collision", src)
		} else if !strings.Contains(err.Error(), "collides with") {
			t.Errorf("expected a name-collision error for %q, got: %v", src, err)
		}
	}
}

const keywordCargoToml = `
[package]
name = "onekit-rust-keyword-fixture"
version = "0.1.0"
edition = "2024"

[dependencies]
axum = "0.8"
base64 = "0.22"
regex = "1"
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
url = "2"
urlencoding = "2"
uuid = "1"
validator = "0.20"
`

// keywordHarness proves the suffix mangling is wire-neutral: a value decoded
// from the original schema names must re-encode to exactly the same JSON.
const keywordHarness = `
#[cfg(test)]
mod generated_tests {
    use super::kwfix::types::*;

    #[test]
    fn keyword_field_names_keep_their_wire_names() {
        let wire = serde_json::json!({
            "self": true, "super": false, "crate": true, "type": false, "ok": true
        });
        let value: Flags = serde_json::from_value(wire.clone()).unwrap();
        assert!(value.self_ && value.crate_ && value.ok);
        assert!(!value.super_ && !value.r#type);
        assert_eq!(serde_json::to_value(&value).unwrap(), wire);
    }

    #[test]
    fn keyword_enum_values_keep_their_wire_names() {
        for (json, expected) in [
            ("self", Mode::Self_),
            ("super", Mode::Super),
            ("crate", Mode::Crate),
            ("normal", Mode::Normal),
        ] {
            let decoded: Mode = serde_json::from_value(serde_json::json!(json)).unwrap();
            assert_eq!(decoded, expected);
            assert_eq!(serde_json::to_value(decoded).unwrap(), serde_json::json!(json));
        }
        assert_eq!(Mode::default(), Mode::Self_);
    }
}
`

// TestGeneratedRustKeywordsCompile is the end-to-end guard: the fixture's
// types, server and client must build under a real rustc, and the mangled Rust
// identifiers must still speak the original JSON wire format.
func TestGeneratedRustKeywordsCompile(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	file := compileKeywordFixture(t).Files[0]

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "kwfix"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	writes := map[string]string{
		"Cargo.toml":          keywordCargoToml,
		"src/lib.rs":          "pub mod kwfix;\n" + keywordHarness,
		"src/kwfix/mod.rs":    "pub mod client;\npub mod server;\npub mod types;\n",
		"src/kwfix/types.rs":  string(GenerateTypes(file)),
		"src/kwfix/server.rs": string(GenerateServer(file)),
		"src/kwfix/client.rs": string(GenerateClient(file)),
	}
	for name, content := range writes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cmd := exec.Command("cargo", "test", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust crate for keyword names failed: %v\n%s", err, out)
	}
}
