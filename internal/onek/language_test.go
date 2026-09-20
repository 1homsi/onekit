package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func languageFixture(t *testing.T) (string, string) {
	t.Helper()
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "onekit.toml"), "module = \"example.com/test\"\nschema_root = \"schema\"\n")
	writeTestFile(t, filepath.Join(root, "schema/common/models.onk"), `package common
/// A user shared across services.
message User { id: string }
enum State { Ready }
message Failure @status(404) { reason: string }
`)
	writeTestFile(t, filepath.Join(root, "schema/other/models.onk"), `package other
message User { other: string }
`)
	source := `package api
import "../common/models.onk"
message Request { id: string }
message Response {
 user: User
 users: map[string, common.User]
 state: State
 payload: oneof(discriminator: "kind") { user: User }
}
service API {
 get(Request) -> Response | Failure @get("/users/{id}")
}
`
	writeTestFile(t, filepath.Join(root, "schema/api/service.onk"), source)
	return root, source
}
func positionOf(t *testing.T, text, needle string) Position {
	t.Helper()
	offset := strings.Index(text, needle)
	if offset < 0 {
		t.Fatalf("missing %q", needle)
	}
	prefix := text[:offset]
	start := strings.LastIndex(prefix, "\n") + 1
	return Position{strings.Count(prefix, "\n"), len(utf16.Encode([]rune(prefix[start:])))}
}
func TestLanguageCompilerBindings(t *testing.T) {
	root, source := languageFixture(t)
	s, err := AnalyzeLanguage(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", s.Diagnostics)
	}
	path := filepath.Join(root, "schema/api/service.onk")
	for _, needle := range []string{"User\n", "common.User", "User }"} {
		symbol := s.SymbolAt(path, positionOf(t, source, needle))
		if symbol == nil || symbol.QualifiedName != "common.User" {
			t.Fatalf("%q resolved to %+v", needle, symbol)
		}
		if symbol.Documentation != "A user shared across services." {
			t.Fatalf("missing docs: %+v", symbol)
		}
		refs := s.References(symbol, false)
		if len(refs) != 3 {
			t.Fatalf("references: %+v", refs)
		}
		if len(s.References(symbol, true)) != 4 {
			t.Fatal("declaration missing")
		}
	}
	for needle, want := range map[string]string{"State\n": "common.State", "Request)": "api.Request", "Response |": "api.Response", "Failure @": "common.Failure"} {
		symbol := s.SymbolAt(path, positionOf(t, source, needle))
		if symbol == nil || symbol.QualifiedName != want {
			t.Fatalf("%s: %+v", needle, symbol)
		}
	}
	if len(s.Search("User", "")) != 7 {
		t.Fatalf("search: %+v", s.Search("User", ""))
	}
	if len(s.Files) != 3 || len(s.Files[0].Imports) != 1 {
		t.Fatalf("files: %+v", s.Files)
	}
	// Scope is enforced by the compiler rather than an independent name lookup.
	bad := strings.Replace(source, "state: State", "state: other.User", 1)
	s, err = AnalyzeLanguage(root, map[string]string{path: bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Diagnostics) == 0 || len(s.refs) != 0 {
		t.Fatalf("invalid bindings leaked: %+v", s)
	}
}
func TestLanguageRefreshAndOverlays(t *testing.T) {
	root, source := languageFixture(t)
	path := filepath.Join(root, "schema/api/service.onk")
	bad := strings.Replace(source, "user: User", "user: Missing", 1)
	s, err := AnalyzeLanguage(root, map[string]string{path: bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Diagnostics) != 1 || s.Diagnostics[0].Path != path || !strings.Contains(s.Diagnostics[0].Message, "Missing") {
		t.Fatalf("diagnostics: %+v", s.Diagnostics)
	}
	if len(s.Search("common.User", "")) != 2 {
		t.Fatal("declarations unavailable")
	}
	writeTestFile(t, path, bad)
	s, err = AnalyzeLanguage(root, nil)
	if err != nil || len(s.Diagnostics) != 1 {
		t.Fatalf("did not refresh: %v %+v", err, s)
	}
	s, err = AnalyzeLanguage(root, map[string]string{path: source})
	if err != nil || len(s.Diagnostics) != 0 {
		t.Fatalf("overlay not applied: %v %+v", err, s)
	}
	// Unsaved new documents participate, and are removed when their overlay closes.
	newPath := filepath.Join(root, "schema/api/new.onk")
	s, err = AnalyzeLanguage(root, map[string]string{path: source, newPath: "message New {}"})
	if err != nil || len(s.Search("New", "")) != 1 {
		t.Fatalf("new document: %v %+v", err, s)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatal("analysis wrote a file")
	}
}
func TestLanguagePositionsAndIsolation(t *testing.T) {
	root, err := canonicalProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "message\nUser { id: string }\nmessage Box { /* 😀 */ user: User }\n"
	path := filepath.Join(root, "schema.onk")
	writeTestFile(t, path, text)
	s, err := AnalyzeLanguage(root, nil)
	if err != nil || len(s.Diagnostics) > 0 {
		t.Fatalf("%v %+v", err, s)
	}
	symbol := s.SymbolAt(path, positionOf(t, text, "User }"))
	if symbol == nil || symbol.Location.Range.Start != (Position{1, 0}) {
		t.Fatalf("wrong name span: %+v", symbol)
	}
	refs := s.References(symbol, false)
	if len(refs) != 1 || refs[0].Range.Start != positionOf(t, text, "User }") {
		t.Fatalf("UTF-16 range: %+v", refs)
	}
	if _, err := AnalyzeLanguage(root, map[string]string{filepath.Join(root, "..", "outside.onk"): "message X {}"}); err == nil {
		t.Fatal("outside overlay accepted")
	}
	if _, err := languagePath(root, "../outside.onk"); err == nil {
		t.Fatal("outside path accepted")
	}
	link := filepath.Join(root, "link.onk")
	if err := os.Symlink(path, link); err != nil {
		t.Skip(err)
	}
	if _, err := languagePath(root, link); err == nil {
		t.Fatal("symlink accepted")
	}
}
