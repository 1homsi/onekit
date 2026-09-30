package gendart

import "testing"

const wsSocketSchema = `
package app
message Blob { room: string limit: int32? @query name: string data: bytes @raw }
service Chat {
  join(Blob) -> Blob @ws("/rooms/{room}")
}
`

const wsSocketMain = `import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:onekit_check/client.dart';

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

Future<void> main() async {
  final seen = <String>[];
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((request) async {
    seen.add('${request.uri.path}?${request.uri.query}|${request.headers.value('x-token')}');
    final socket = await WebSocketTransformer.upgrade(request);
    socket.listen((data) async {
      final text = data is String ? data : 'binary:${(data as List<int>).length}';
      if (text == '{"name":"big"}') {
        socket.add('x' * 2000);
        return;
      }
      socket.add(data);
      if (text.startsWith('{"name":"bye"')) await socket.close(1000, 'done');
    });
  });
  final client = ChatClient('http://127.0.0.1:${server.port}', headers: {'x-token': 'secret'}, maxWsFrameBytes: 1024);
  final socket = await client.join(Blob(room: 'r1', limit: 3));
  check(seen.single == '/rooms/r1?limit=3|secret', 'handshake: $seen');

  try {
    await socket.receive(timeout: const Duration(milliseconds: 50));
    check(false, 'expected timeout');
  } on WsTimeoutException {
    check(true, 'timeout');
  }
  await socket.send(Blob(name: 'plain'));
  check((await socket.receive()).name == 'plain', 'text echo');
  await socket.send(Blob(name: 'raw', data: Uint8List.fromList([0, 1, 2])));
  final raw = await socket.receive();
  check(raw.name == 'raw' && raw.data.length == 3 && raw.data[2] == 2, 'raw echo: $raw');
  await socket.send(Blob(name: 'bye'));
  final names = <String>[];
  await for (final frame in socket.frames) {
    names.add(frame.name);
  }
  check(names.join(',') == 'bye', 'frames ended on close: $names');
  try {
    await socket.send(Blob(name: 'late'));
    check(false, 'send after close');
  } on WsClosedException catch (e) {
    check(e.code == 1000, 'close code: $e');
  }

  final limited = await client.join(Blob(room: 'r2'));
  await limited.send(Blob(name: 'big'));
  try {
    await limited.receive(timeout: const Duration(seconds: 5));
    check(false, 'expected 1009');
  } on WsClosedException catch (e) {
    check(e.code == 1009, 'frame limit: $e');
  }
  client.close();
  await server.close(force: true);
  print('OK');
}
`

func TestDartWebSocketFrameSocket(t *testing.T) {
	runDartSchema(t, wsSocketSchema, wsSocketMain)
}
