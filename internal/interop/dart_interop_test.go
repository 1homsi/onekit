package interop

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/gendart"
)

const dartPubspec = `name: onekit_interop
environment:
  sdk: ^3.4.0
dependencies:
  http: ^1.2.0
  web_socket_channel: ^3.0.0
`

const dartClientHarness = `import 'dart:io';
import 'dart:typed_data';

import 'package:onekit_interop/client.dart';

final big = '{"k":"v\\n"},' * (30 * 1024);

Never fail(Object message) {
  stderr.writeln(message);
  exit(1);
}

Frame run(String code) => Frame(payload: FramePayloadRun(RunRequest(code: code)));

Frame answer(HostCall call) => Frame(payload: FramePayloadHostResult(HostResult(id: call.id, value: 'answer')));

Future<RunResult> untilResult(WsCallSocket<Frame, Frame> socket) async {
  while (true) {
    final payload = (await socket.receive(timeout: const Duration(seconds: 20))).payload;
    if (payload is FramePayloadHostCall) {
      if (payload.hostCall.method != 'doThing') fail('host_call body did not decode: ${payload.hostCall}');
      await socket.send(answer(payload.hostCall));
      continue;
    }
    if (payload is FramePayloadRunResult) return payload.runResult;
    fail('unexpected frame: $payload');
  }
}

bool sameBytes(Uint8List a, List<int> b) => a.length == b.length && [for (var i = 0; i < a.length; i++) a[i] == b[i]].every((x) => x);

Future<void> main(List<String> args) async {
  final client = RuntimeClient(args[0]);
  final socket = await client.execute(run(big));
  await socket.send(run(big));
  final result = await untilResult(socket);
  if (result.exitCode != 7 || result.resultJson != 'R:$big') fail('raw string did not round trip: ${result.resultJson.length}');
  final chunks = result.chunks.map((c) => c.data).toList();
  if (chunks.length != 3 || !sameBytes(chunks[0], [0, 1, 2]) || chunks[1].isNotEmpty || !sameBytes(chunks[2], List.filled(1000, 0xff))) {
    fail('raw bytes did not round trip: ${chunks.map((c) => c.length).toList()}');
  }

  final huge = 'h' * (20 << 20);
  await socket.send(run(huge));
  if ((await untilResult(socket)).resultJson != 'R:$huge') fail('chunked round trip failed');

  try {
    await socket.call('c-1', Frame(payload: FramePayloadHostCall(HostCall(id: 'c-1', method: 'slow'))), timeout: const Duration(milliseconds: 100));
    fail('abandoned call returned');
  } on WsTimeoutException {
    stdout.flush();
  }
  final after = (await socket.receive(timeout: const Duration(seconds: 10))).payload;
  if (after is! FramePayloadRunResult || after.runResult.exitCode != 9) fail('server never saw the cancel: $after');
  await socket.close();

  final second = await client.execute(Frame());
  await second.send(run('x'));
  var sawResult = false;
  await for (final frame in second.frames) {
    final payload = frame.payload;
    if (payload is FramePayloadHostCall) await second.send(answer(payload.hostCall));
    if (payload is FramePayloadRunResult) {
      sawResult = true;
      break;
    }
  }
  if (!sawResult) fail('iteration ended before run_result');
  await second.close();
  client.close();
  print('OK');
}
`

func TestWSDartClientGoServer(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart not available")
	}
	goBin := buildGoHarness(t)
	file := compileSchema(t)
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	writeFile(t, filepath.Join(dir, "pubspec.yaml"), dartPubspec)
	writeFile(t, filepath.Join(lib, "onekit.dart"), string(gendart.GenerateRuntime()))
	writeFile(t, filepath.Join(lib, "onekit_ws.dart"), string(gendart.GenerateWSRuntime()))
	writeFile(t, filepath.Join(lib, "onekit_ws_io.dart"), string(gendart.GenerateWSConnectIO()))
	writeFile(t, filepath.Join(lib, "onekit_ws_web.dart"), string(gendart.GenerateWSConnectWeb()))
	writeFile(t, filepath.Join(lib, "models.dart"), string(gendart.GenerateTypes(file)))
	writeFile(t, filepath.Join(lib, "client.dart"), string(gendart.GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "bin", "harness.dart"), dartClientHarness)
	run(t, dir, "dart", "pub", "get")
	port := startServer(t, "", goBin, "server")
	expectOK(t, dir, "dart", "run", "bin/harness.dart", "http://127.0.0.1:"+port)
}
