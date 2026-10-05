package gendart

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

func TestDartInt64NumberEncodingIsOnTheWire(t *testing.T) {
	ast, err := onklang.Parse(`package app
message Item {
  id: int64
  big: uint64
  ids: int64[]
  maybe: int64?
  by_name: map[string, int64]
  keep: int64 @encode("number")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.onk", AST: ast}}, onkcompile.CompileOptions{Int64Encoding: "number"})
	if err != nil {
		t.Fatal(err)
	}
	dir := dartPackage(t, pkg.Files[0])
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), `
import 'dart:convert';
import 'package:onekit_check/models.dart';

void main() {
  final wire = {'id': 5, 'big': 7, 'ids': [1, 2], 'maybe': 9, 'by_name': {'a': 4}, 'keep': 3};
  final item = Item.fromJson(wire);
  if (item.validate().isNotEmpty) throw 'rejected: ${item.validate()}';
  final out = item.toJson();
  if (jsonEncode(Map.fromEntries(out.entries.toList()..sort((a, b) => a.key.compareTo(b.key)))) !=
      jsonEncode(Map.fromEntries(wire.entries.toList()..sort((a, b) => a.key.compareTo(b.key))))) {
    throw 'wire $out, want $wire';
  }
  print('OK');
}
`)
	out := dartCommand(t, dir, "run", "bin/main.dart")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("dart run: %s", out)
	}
}
