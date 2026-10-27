package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const rustInt64Harness = `

#[cfg(test)]
mod int64_number_tests {
    use super::*;

    #[test]
    fn int64_fields_are_json_numbers() {
        let wire = serde_json::json!({"id": 5, "big": 7, "ids": [1, 2], "maybe": 9, "by_name": {"a": 4}, "keep": 3});
        let item: Item = serde_json::from_value(wire.clone()).expect("numbers decode");
        assert_eq!(item.id, 5);
        assert_eq!(item.ids, vec![1, 2]);
        assert_eq!(item.maybe, Some(9));
        assert_eq!(serde_json::to_value(&item).unwrap(), wire);
    }
}
`

func TestRustInt64NumberEncodingIsOnTheWire(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	ast, err := onktest.Parse(`namespace app;

model Item {
  @encode(string)
  id: int64;
  @encode(string)
  big: uint64;
  @Onkt.encode("string")
  ids: int64[];
  @encode(string)
  maybe?: int64;
  by_name: Record<int64>;
  keep: int64;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{Int64Encoding: "number"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRustFile(t, filepath.Join(dir, "Cargo.toml"), strings.Replace(rustRulesCargoToml, "NAME", "onekit-rust-int64-number", 1))
	writeRustFile(t, filepath.Join(dir, "src", "lib.rs"), string(GenerateTypes(pkg.Files[0]))+rustInt64Harness)
	if out, err := cargoCommand(dir, "test", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
