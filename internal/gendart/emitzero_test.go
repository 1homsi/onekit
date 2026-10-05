package gendart

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const emitZeroSchema = `package app

enum Level { LOW HIGH }

message Inner { v: string }

message Item {
  name: string
  flag: bool
  count: int32
  big: int64
  huge: uint64
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

func TestDartEmitZeroValuesWritesEveryNonOptionalField(t *testing.T) {
	ast, err := onklang.Parse(emitZeroSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{EmitZeroValues: true})
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
