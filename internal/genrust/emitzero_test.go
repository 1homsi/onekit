package genrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const rustEmitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

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
	ast, err := onktest.Parse(rustEmitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
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
