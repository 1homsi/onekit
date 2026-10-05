package genrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const rustEmitZeroSchema = `package app

enum Level { LOW HIGH }

message Inner { v: string }

message Item {
  name: string
  flag: bool
  count: int32
  big: int64
  ratio: float64
  level: Level
  num_level: Level @encode("number")
  tags: string[]
  ids: int64[]
  items: Inner[]
  by_name: map[string, string]
  data: bytes
  hex: bytes @encode("hex")
  maybe: string?
  maybe_n: int32?
  inner: Inner
}
`

const rustEmitZeroHarness = `

#[cfg(test)]
mod emit_zero_tests {
    use super::*;

    #[test]
    fn zero_values_are_written() {
        let zero = serde_json::json!({
            "name": "", "flag": false, "count": 0, "big": "0", "ratio": 0.0, "level": "LOW", "num_level": 0,
            "tags": [], "ids": [], "items": [], "by_name": {}, "data": "", "hex": ""
        });
        let strip = |mut value: serde_json::Value| {
            value.as_object_mut().unwrap().remove("inner");
            value
        };
        assert_eq!(strip(serde_json::to_value(Item::default()).unwrap()), zero);
        let back: Item = serde_json::from_value(zero.clone()).unwrap();
        assert_eq!(strip(serde_json::to_value(&back).unwrap()), zero);
        let set = serde_json::to_value(Item { name: "n".into(), maybe: Some("x".into()), ..Default::default() }).unwrap();
        assert_eq!(set["name"], "n");
        assert_eq!(set["maybe"], "x");
        assert!(set.get("maybe_n").is_none());
        assert!(set.get("inner").is_none_or(|inner| inner.is_null()));
    }
}
`

func TestRustEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	t.Parallel()
	ast, err := onklang.Parse(rustEmitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRustFile(t, filepath.Join(dir, "Cargo.toml"), strings.Replace(rustRulesCargoToml, "NAME", "onekit-rust-emit-zero", 1))
	writeRustFile(t, filepath.Join(dir, "src", "lib.rs"), string(GenerateTypes(pkg.Files[0]))+rustEmitZeroHarness)
	if out, err := cargoCommand(dir, "test", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
