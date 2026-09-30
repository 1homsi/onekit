package gendart

import "testing"

const edgeSchema = `
package app
enum Kind { NEW DEFAULT VALUES }
message Empty {}
message Inner { note: string }
message Outer {
  message Child { label: string }
  enum Mode { FAST SLOW }
  child: Child
  mode: Mode
}
message Awkward {
  switch: string
  var: int32
  values: string[]
  kind: Kind
  maybe_kind: Kind?
  maybe_bytes: bytes?
  maybe_big: int64?
  maybe_huge: uint64?
  num_big: int64 @encode("number")
  millis: timestamp @encode("unix_millis")
  url_bytes: bytes @encode("base64url_raw")
  null_empty: Inner @empty("null")
  omit_empty: Inner @empty("omit")
  size: int32 @in(10, 25)
  huge_in: uint64? @in(1, 2)
  pick: oneof(flatten: true) {
    inner: Inner @tag("inner")
  }
  alt: oneof(discriminator: "t") {
    blob: bytes @tag("blob")
    kind_ref: Kind @tag("kind")
    empty: Empty @tag("empty")
  } @required
  must: string? @required
  must_list: string[] @required
  must_inner: Inner @required
}
message Ids { v: int64[] @unwrap }
message Lookup { v: map[string, Inner] @unwrap }
message Wrapped { v: Inner @unwrap }
message Problem @status(409) { reasons: string[] details: map[string, string] }
service Weird {
  close(Empty) -> Empty @post("/close")
  get(Awkward) -> Awkward | Problem @post("/get")
}
`

const edgeMain = `import 'dart:convert';
import 'dart:typed_data';

import 'package:onekit_check/client.dart';

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

void main() {
  check(Kind.new_.wire == 'NEW' && Kind.default_.wire == 'DEFAULT' && Kind.values_.wire == 'VALUES', 'enum names');
  check(OuterMode.fast.wire == 'FAST', 'nested enum');
  final outer = Outer(child: OuterChild(label: 'x'), mode: OuterMode.slow);
  check(jsonEncode(outer.toJson()) == '{"child":{"label":"x"},"mode":"SLOW"}', 'nested: ${jsonEncode(outer.toJson())}');
  check(Outer.fromJson(outer.toJson()) == outer, 'nested round trip');
  check(jsonEncode(Empty().toJson()) == '{}', 'empty');

  final a = Awkward(
    switch_: 'x',
    var_: 3,
    values: ['v'],
    kind: Kind.default_,
    maybeKind: Kind.values_,
    maybeBytes: Uint8List(0),
    maybeBig: 0,
    maybeHuge: BigInt.from(7),
    numBig: 9,
    millis: DateTime.fromMillisecondsSinceEpoch(1500, isUtc: true),
    urlBytes: Uint8List.fromList([251, 255]),
    nullEmpty: Inner(),
    omitEmpty: Inner(),
    size: 25,
    hugeIn: BigInt.two,
    pick: AwkwardPickInner(Inner(note: 'flat')),
    alt: AwkwardAltBlob(Uint8List.fromList([1])),
    must: 'yes',
    mustList: ['y'],
    mustInner: Inner(note: 'n'),
  );
  final wire = a.toJson();
  final expected = {
    'switch': 'x',
    'var': 3,
    'values': ['v'],
    'kind': 'DEFAULT',
    'maybe_kind': 'VALUES',
    'maybe_bytes': '',
    'maybe_big': '0',
    'maybe_huge': '7',
    'num_big': 9,
    'millis': 1500,
    'url_bytes': '-_8',
    'null_empty': null,
    'size': 25,
    'huge_in': '2',
    'pick': {'note': 'flat', 'type': 'inner'},
    'alt': {'t': 'blob', 'blob': 'AQ=='},
    'must': 'yes',
    'must_list': ['y'],
    'must_inner': {'note': 'n'},
  };
  check(jsonEncode(wire) == jsonEncode(expected), 'wire: ${jsonEncode(wire)}');
  check(Awkward.fromJson(jsonDecode(jsonEncode(wire))) == a, 'round trip');
  check(a.validate().isEmpty, 'valid: ${a.validate()}');
  check(Awkward.fromJson({'alt': {'t': 'kind', 'kind': 'NEW'}}).alt == AwkwardAltKindRef(Kind.new_), 'enum variant');
  check(Awkward.fromJson({'alt': {'t': 'empty', 'empty': {}}}).alt == AwkwardAltEmpty(Empty()), 'empty variant');

  final violations = Awkward(size: 3, hugeIn: BigInt.from(5)).validate();
  for (final want in [
    'size must be one of the allowed values',
    'huge_in must be one of the allowed values',
    'alt is required',
    'must is required',
    'must_list is required',
    'must_inner is required',
  ]) {
    check(violations.contains(want), 'missing $want in $violations');
  }
  check(Awkward(size: 10, alt: AwkwardAltBlob(Uint8List(0)), must: '', mustList: ['a'], mustInner: Inner()).validate().contains('must is required'), 'empty required string');

  check(jsonEncode(Ids(v: [1]).toJson()) == '["1"]', 'unwrap list');
  check(jsonEncode(Lookup(v: {'k': Inner(note: 'z')}).toJson()) == '{"k":{"note":"z"}}', 'unwrap map');
  check(Lookup.fromJson({'k': {'note': 'z'}}).v['k']!.note == 'z', 'unwrap map decode');
  check(jsonEncode(Wrapped(v: Inner(note: 'w')).toJson()) == '{"note":"w"}', 'unwrap message');
  check(Wrapped.fromJson({'note': 'w'}).v!.note == 'w', 'unwrap message decode');

  final problem = Problem.fromJson({'reasons': ['a'], 'details': {'k': 'v'}});
  check(problem.reasons.single == 'a' && problem.details['k'] == 'v', 'error class');
  check(problem is Exception, 'error implements Exception');
  check(Problem() == Problem(reasons: []), 'error equality');
  final client = WeirdClient('http://127.0.0.1:1');
  check(client.close_ is Function && client.get is Function, 'method names');
  client.close();
  print('OK');
}
`

func TestDartNamingAndUncommonShapes(t *testing.T) {
	runDartSchema(t, edgeSchema, edgeMain)
}
