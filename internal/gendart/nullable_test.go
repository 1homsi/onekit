package gendart

import (
	"path/filepath"
	"strings"
	"testing"
)

const nullableSchema = `package app

message Item { name: string }
message Patch {
  id: int32
  folder_id: int64? @nullable @encode("number")
  note: string? @nullable
  item: Item? @nullable
}
`

func TestDartNullableFieldsKeepAbsentNullAndValueApart(t *testing.T) {
	file := compileDartSchema(t, nullableSchema)
	dir := dartPackage(t, file)
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), `
import 'dart:convert';
import 'package:onekit_check/models.dart';

String canon(Map<String, dynamic> m) => jsonEncode(Map.fromEntries(m.entries.toList()..sort((a, b) => a.key.compareTo(b.key))));

void main() {
  if (canon(Patch(id: 1).toJson()) != '{"id":1}') throw 'absent: ${Patch(id: 1).toJson()}';
  final nulls = Patch(id: 1, folderIdNull: true, noteNull: true, itemNull: true).toJson();
  if (canon(nulls) != '{"folder_id":null,"id":1,"item":null,"note":null}') throw 'nulls: $nulls';
  final values = Patch(id: 1, folderId: 7, note: 'n', item: Item(name: 'x')).toJson();
  if (values['folder_id'] != 7 || values['note'] != 'n' || (values['item'] as Map)['name'] != 'x') throw 'values: $values';
  final both = Patch(id: 1, note: 'n', noteNull: true).toJson();
  if (both['note'] != 'n') throw 'a value wins over the null flag: $both';

  final decoded = Patch.fromJson({'id': 1, 'folder_id': null, 'note': 'x'});
  if (!decoded.folderIdNull || decoded.folderId != null) throw 'null decode';
  if (decoded.noteNull || decoded.note != 'x') throw 'value decode';
  if (decoded.itemNull || decoded.item != null) throw 'absent decode';
  if (Patch.fromJson({'id': 1}).folderIdNull) throw 'absent must not read as null';
  print('OK');
}
`)
	out := dartCommand(t, dir, "run", "bin/main.dart")
	if strings.TrimSpace(out) != "OK" {
		t.Fatalf("dart run: %s", out)
	}
}
