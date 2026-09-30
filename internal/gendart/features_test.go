package gendart

import "testing"

const featureSchema = `
package app
/// Visibility.
enum Visibility { PRIVATE PUBLIC_VIEW }
message Meta { owner: string @email }
message Note {
  /// The id.
  id: string @uuid
  title: string @len(1, 20)
  count: int32 @range(0, 100)
  big: int64
  huge: uint64
  ratio: float64 @gte(0)
  tags: string[] @max_items(3)
  data: bytes
  hex: bytes @encode("hex")
  at: timestamp
  day: timestamp @encode("date")
  secs: timestamp @encode("unix_seconds")
  vis: Visibility
  vis_num: Visibility @encode("number")
  maybe: string?
  meta: Meta
  flat: Meta @flatten(prefix: "m_")
  labels: map[string, int64]
  raw_json: json
  old: string @deprecated("use title")
  body: oneof(discriminator: "kind") {
    text: string @tag("text")
    meta_ref: Meta @tag("meta")
    n: int64 @tag("n")
  }
}
message Ids { v: int64[] @unwrap }
message GetNote { id: string @uuid limit: int32? @query tags: string[] @query }
message NotFound @status(404) { code: string }
/// Notes API.
service Notes {
  /// Fetch.
  get(GetNote) -> Note | NotFound @get("/notes/{id}")
  create(Note) -> Note @post("/notes")
  ids(GetNote) -> Ids @get("/ids/{id}")
  watch(GetNote) -> Note | NotFound @get("/watch/{id}") @stream
  legacy(GetNote) -> Note @get("/legacy/{id}") @deprecated
}
`

const featureMain = `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:onekit_check/client.dart';

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

Future<void> main() async {
  final note = Note(
    id: '0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f',
    title: 'hi',
    big: -9007199254740993,
    huge: BigInt.parse('18446744073709551615'),
    tags: ['a'],
    data: Uint8List.fromList([1, 2, 3]),
    hex: Uint8List.fromList([255, 1]),
    at: DateTime.utc(2026, 1, 2, 3, 4, 5, 6),
    day: DateTime.utc(2026, 1, 2),
    secs: DateTime.utc(2026, 1, 2, 3, 4, 5),
    vis: Visibility.publicView,
    visNum: Visibility.publicView,
    meta: Meta(owner: 'a@b.io'),
    flat: Meta(owner: 'c@d.io'),
    labels: {'x': 5},
    rawJson: {'free': [1, true]},
    body: NoteBodyN(42),
  );
  final wire = note.toJson();
  final expected = {
    'id': '0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f',
    'title': 'hi',
    'big': '-9007199254740993',
    'huge': '18446744073709551615',
    'tags': ['a'],
    'data': 'AQID',
    'hex': 'ff01',
    'at': '2026-01-02T03:04:05.006Z',
    'day': '2026-01-02',
    'secs': 1767323045,
    'vis': 'PUBLIC_VIEW',
    'vis_num': 1,
    'meta': {'owner': 'a@b.io'},
    'm_owner': 'c@d.io',
    'labels': {'x': 5},
    'raw_json': {'free': [1, true]},
    'body': {'kind': 'n', 'n': '42'},
  };
  check(jsonEncode(wire) == jsonEncode(expected), 'wire mismatch: ${jsonEncode(wire)}');
  final back = Note.fromJson(jsonDecode(jsonEncode(wire)));
  check(back == note, 'round trip: $back');
  check(back.body is NoteBodyN && (back.body as NoteBodyN).n == 42, 'oneof decode');
  check(Note.fromJson({'body': {'kind': 'meta', 'meta_ref': {'owner': 'x@y.io'}}}).body == NoteBodyMetaRef(Meta(owner: 'x@y.io')), 'message variant');
  check(Note.fromJson({'body': {'kind': 'nope'}}).body == null, 'unknown variant');
  check(Note.fromJson({'at': '2026-01-02T03:04:05.123456789Z'}).at == DateTime.utc(2026, 1, 2, 3, 4, 5, 123, 456), 'nanos');
  check(note.validate().isEmpty, 'valid note: ${note.validate()}');
  final bad = Note(id: 'nope', title: '', count: 101, ratio: -1, tags: ['a', 'b', 'c', 'd'], meta: Meta(owner: 'bad'));
  final violations = bad.validate();
  for (final want in ['id must be a valid UUID', 'title has invalid length', 'count violates @range', 'ratio violates @gte', 'tags must contain at most 3 items', 'meta: owner must be a valid email']) {
    check(violations.contains(want), 'missing $want in $violations');
  }
  check(jsonEncode(Ids(v: [1, -2]).toJson()) == '["1","-2"]', 'unwrap strings');
  check(Ids.fromJson(['3', 4]).v.join(',') == '3,4', 'unwrap decode');
  check(Visibility.fromWire('PRIVATE') == Visibility.private, 'enum wire');

  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((request) async {
    final path = request.uri.path;
    final response = request.response;
    if (path == '/notes/0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f') {
      check(request.uri.queryParametersAll['tags']!.join(',') == 'a,b', 'repeated query');
      check(request.uri.queryParameters['limit'] == '5', 'optional query');
      check(request.headers.value('x-trace') == 't1' && request.headers.value('x-base') == 'b', 'headers');
      response.write(jsonEncode({'title': 'found', 'big': '7'}));
    } else if (path.startsWith('/notes/')) {
      response.statusCode = 404;
      response.write(jsonEncode({'code': 'missing'}));
    } else if (path == '/notes' && request.method == 'POST') {
      final body = await utf8.decoder.bind(request).join();
      response.write(body);
    } else if (path.startsWith('/watch/')) {
      response.headers.contentType = ContentType('text', 'event-stream');
      response.write('data: {"title":"one"}\r\n\r\n: comment\n\ndata: {"title":\ndata: "two"}\n\nevent: error\ndata: {"message":"boom"}\n\n');
    } else if (path.startsWith('/slow/')) {
      await Future<void>.delayed(const Duration(seconds: 2));
    } else {
      response.statusCode = 500;
      response.write('plain failure');
    }
    await response.close();
  });
  final client = NotesClient('http://127.0.0.1:${server.port}/', headers: {'x-base': 'b', 'x-trace': 'base'});
  final got = await client.get(GetNote(id: '0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f', limit: 5, tags: ['a', 'b']), headers: {'x-trace': 't1'});
  check(got.title == 'found' && got.big == 7, 'get: $got');
  try {
    await client.get(GetNote(id: '1f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f'));
    check(false, 'expected NotFound');
  } on NotFound catch (e) {
    check(e.code == 'missing', 'typed error');
  }
  try {
    await client.get(GetNote(id: 'bad'));
    check(false, 'expected validation error');
  } on RequestValidationException catch (e) {
    check(e.violations.contains('id must be a valid UUID'), 'validation: ${e.violations}');
  }
  final echoed = await client.create(note);
  check(echoed == note, 'post round trip: $echoed');
  final events = <String>[];
  try {
    await for (final event in client.watch(GetNote(id: '0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f'))) {
      events.add(event.title);
    }
    check(false, 'expected stream error');
  } on StreamException catch (e) {
    check((e.payload as Map)['message'] == 'boom', 'stream error payload');
  }
  check(events.join(',') == 'one,two', 'sse events: $events');
  try {
    await client.ids(GetNote(id: '0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f'));
    check(false, 'expected unexpected status');
  } on UnexpectedStatusException catch (e) {
    check(e.status == 500 && e.bodyText == 'plain failure', 'unexpected status');
  }
  client.close();
  await server.close(force: true);
  print('OK');
}
`

func TestDartClientCoversEveryFieldKind(t *testing.T) {
	runDartSchema(t, featureSchema, featureMain)
}
