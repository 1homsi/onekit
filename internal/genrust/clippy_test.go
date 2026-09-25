package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestGeneratedRustIsWarningFree(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	if err := exec.Command("cargo", "clippy", "--version").Run(); err != nil {
		t.Skip("clippy not available")
	}
	ast, err := onklang.Parse(`
package app
enum Status { ACTIVE  GONE }
message Item { id: string @uuid  status: Status  tags: string[] }
message ListItems { limit: int32 @query }
service Items {
  headers: { "X-Key": string @required @format("uuid") }
  get(Item) -> Item @get("/items/{id}")
  list(ListItems) -> Item @get("/items")
  create(Item) -> Item @post("/items")
  watch(ListItems) -> Item @get("/items/watch") @stream
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	dir := t.TempDir()
	files := map[string]string{
		"Cargo.toml":              strings.Replace(rustWSCargoToml, "onekit-rust-ws-fixture", "onekit-rust-clippy", 1),
		"src/main.rs":             rustBuildOnlyMain,
		"src/generated/mod.rs":    "pub mod types;\npub mod server;\npub mod client;\n",
		"src/generated/types.rs":  string(GenerateTypes(file)),
		"src/generated/server.rs": string(GenerateServer(file)),
		"src/generated/client.rs": string(GenerateClient(file)),
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := cargoCommand(dir, "clippy", "--quiet", "--", "-D", "warnings", "-A", "dead_code").CombinedOutput()
	if err != nil {
		t.Fatalf("generated Rust has warnings: %v\n%s", err, out)
	}
}
