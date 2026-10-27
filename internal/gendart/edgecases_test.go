package gendart

import "testing"

const edgeSchema = `namespace app;

enum Kind {
  NEW,
  DEFAULT,
  VALUES,
}

model Empty {}

model Inner {
  note: string;
}

model Outer {
  child: Child;
  mode: Mode;
}

@Onkt.parent(Outer)
model Child {
  label: string;
}

@Onkt.parent(Outer)
enum Mode {
  FAST,
  SLOW,
}

@discriminated(#{ discriminatorPropertyName: "type", envelope: "none" })
union AwkwardPick {
  @Onkt.tag("inner") inner: Inner,
}

@Onkt.discriminator("t")
union AwkwardAlt {
  @Onkt.tag("blob") blob: bytes,
  @Onkt.tag("kind") kind_ref: Kind,
  @Onkt.tag("empty") empty: Empty,
}

model Awkward {
  switch: string;
  var: int32;
  values: string[];
  kind: Kind;
  maybe_kind?: Kind;
  maybe_bytes?: bytes;
  @encode(string)
  maybe_big?: int64;
  @encode(string)
  maybe_huge?: uint64;
  num_big: int64;
  @Onkt.encode("unix_millis")
  millis: utcDateTime;
  @Onkt.encode("base64url_raw")
  url_bytes: bytes;
  @Onkt.empty("null")
  null_empty: Inner;
  @Onkt.empty("omit")
  omit_empty: Inner;
  @Onkt.values(10, 25)
  size: int32;
  @Onkt.values(1, 2)
  @encode(string)
  huge_in?: uint64;
  pick: AwkwardPick;
  @Onkt.required
  alt: AwkwardAlt;
  @Onkt.required
  must?: string;
  @Onkt.required
  must_list: string[];
  @Onkt.required
  must_inner: Inner;
}

model Ids {
  @Onkt.unwrap
  @Onkt.encode("string")
  v: int64[];
}

model Lookup {
  @Onkt.unwrap
  v: Record<Inner>;
}

model Wrapped {
  @Onkt.unwrap
  v: Inner;
}

@error
model Problem {
  @statusCode _: 409;
  reasons: string[];
  details: Record<string>;
}

interface Weird {
  @post
  @route("/close")
  close(...Empty): Empty;

  @post
  @route("/get")
  get(...Awkward): Awkward | Problem;
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
