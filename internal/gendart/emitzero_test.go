package gendart

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const emitZeroSchema = "namespace app;\n\nenum Level {\n  LOW,\n  HIGH,\n}\n\nmodel Inner {\n  v: string;\n}\n\nmodel Item {\n  name: string;\n  `flag`: boolean;\n  count: int32;\n  @encode(string)\n  big: int64;\n  @encode(string)\n  huge: uint64;\n  ratio: float64;\n  level: Level;\n  @Onkt.encode(\"number\")\n  num_level: Level;\n  tags: string[];\n  @Onkt.encode(\"string\")\n  ids: int64[];\n  items: Inner[];\n  by_name: Record<string>;\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  maybe?: string;\n  maybe_n?: int32;\n  inner: Inner;\n}\n"

func TestDartEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	ast, err := onktest.Parse(emitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := dartPackage(t, pkg.Files[0])
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), `
import 'dart:convert';
import 'package:onekit_check/models.dart';

String canon(Map<String, dynamic> m) => jsonEncode(Map.fromEntries(m.entries.toList()..sort((a, b) => a.key.compareTo(b.key))));

void main() {
  final zero = {
    'name': '', 'flag': false, 'count': 0, 'big': '0', 'huge': '0', 'ratio': 0.0, 'level': 'LOW', 'num_level': 0,
    'tags': [], 'ids': [], 'items': [], 'by_name': {}, 'data': '', 'hex': '',
  };
  final got = Item().toJson();
  if (canon(got) != canon(zero)) throw 'zero value wire ${canon(got)}, want ${canon(zero)}';
  final set = Item(name: 'n', maybe: 'x').toJson();
  if (set['name'] != 'n' || set['maybe'] != 'x') throw 'set values: $set';
  if (set.containsKey('maybe_n') || set.containsKey('inner')) throw 'unset optionals must stay omitted: $set';
  print('OK');
}
`)
	out := dartCommand(t, dir, "run", "bin/main.dart")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("dart run: %s", out)
	}
}
