package interop

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

const httpWireSchema = "namespace notes;\n\nenum Visibility {\n  PRIVATE,\n  PUBLIC_VIEW,\n}\n\nmodel Meta {\n  owner: string;\n}\n\n@Onkt.discriminator(\"kind\")\nunion NoteBody {\n  @Onkt.tag(\"text\") text: string,\n  @Onkt.tag(\"meta\") meta_ref: Meta,\n  @Onkt.tag(\"n\") n: int64,\n}\n\nmodel Note {\n  id: string;\n  count: int32;\n  @encode(string)\n  big: int64;\n  @encode(string)\n  huge: uint64;\n  ratio: float64;\n  tags: string[];\n  data: bytes;\n  @Onkt.encode(\"hex\")\n  hex: bytes;\n  at: utcDateTime;\n  day: plainDate;\n  @encode(\"unixTimestamp\", int64)\n  secs: utcDateTime;\n  vis: Visibility;\n  @Onkt.encode(\"number\")\n  vis_num: Visibility;\n  maybe?: string;\n  meta: Meta;\n  @Onkt.flatten(\"m_\")\n  flat: Meta;\n  labels: Record<string>;\n  raw_json: unknown;\n  body: NoteBody;\n}\n\nmodel Wide {\n  @Onkt.encode(\"string\")\n  nums: int64[];\n  @Onkt.encode(\"string\")\n  ids: uint64[];\n  by_name: Record<int64>;\n  stamps: utcDateTime[];\n  blobs: bytes[];\n  levels: Visibility[];\n  metas: Meta[];\n  meta_map: Record<Meta>;\n  @encode(string)\n  opt_num?: int64;\n  `flag`: boolean;\n  opt_flag?: boolean;\n  small: int32;\n  f32: float32;\n  zero_str: string;\n}\n\nmodel Fetch {\n  id: string;\n  @query\n  limit?: int32;\n  @query\n  tags: string[];\n}\n\n@error\nmodel NotFound {\n  @statusCode _: 404;\n  code: string;\n}\n\ninterface Notes {\n  @post\n  @route(\"/notes\")\n  create(...Note): Note;\n\n  @post\n  @route(\"/wide\")\n  echoWide(...Wide): Wide;\n\n  @get\n  @route(\"/notes/{id}\")\n  fetch(...Fetch): Note | NotFound;\n\n  @get\n  @route(\"/watch/{id}\")\n  @Onkt.stream\n  watch(...Fetch): Note | NotFound;\n}\n"

const goHTTPHarnessMain = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"interop/notes"
)

type impl struct{}

func sample() *notes.Note {
	return &notes.Note{
		Id: "n-1", Count: 3, Big: -9007199254740993, Huge: 18446744073709551615, Ratio: 1.5,
		Tags: []string{"a", "b"}, Data: []byte{1, 2, 3}, Hex: []byte{0xff, 0x01},
		At:   time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC),
		Day:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Secs: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Vis:  notes.VisibilityPublicView, VisNum: notes.VisibilityPublicView,
		Meta: &notes.Meta{Owner: "a@b.io"}, Flat: &notes.Meta{Owner: "c@d.io"},
		Labels: map[string]string{"x": "y"}, RawJson: json.RawMessage(` + "`" + `{"free":[1,true]}` + "`" + `),
		Body: &notes.NoteBodyN{N: 42},
	}
}

func (impl) Create(ctx context.Context, req *notes.Note) (*notes.Note, error) { return req, nil }

func (impl) EchoWide(ctx context.Context, req *notes.Wide) (*notes.Wide, error) { return req, nil }

func (impl) Fetch(ctx context.Context, req *notes.Fetch) (*notes.Note, error) {
	if req.Id == "missing" {
		return nil, &notes.NotFound{Code: "gone"}
	}
	note := sample()
	note.Id = fmt.Sprintf("%s|%d|%s", req.Id, *req.Limit, strings.Join(req.Tags, ","))
	return note, nil
}

func (impl) Watch(ctx context.Context, req *notes.Fetch, sender notes.SSESender) error {
	for _, id := range []string{"one", "two"} {
		note := sample()
		note.Id = id
		if err := sender.Send(note); err != nil {
			return err
		}
	}
	return &notes.NotFound{Code: "done"}
}

func main() {
	mux := http.NewServeMux()
	if err := notes.RegisterNotesServer(mux, impl{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("PORT=%d\n", listener.Addr().(*net.TCPAddr).Port)
	fmt.Fprintln(os.Stderr, http.Serve(listener, mux))
	os.Exit(1)
}
`

const dartHTTPHarness = `import 'dart:io';
import 'dart:typed_data';

import 'package:onekit_interop/client.dart';

Never fail(Object message) {
  stderr.writeln(message);
  exit(1);
}

Note sample(String id) => Note(
      id: id,
      count: 3,
      big: -9007199254740993,
      huge: BigInt.parse('18446744073709551615'),
      ratio: 1.5,
      tags: ['a', 'b'],
      data: Uint8List.fromList([1, 2, 3]),
      hex: Uint8List.fromList([0xff, 0x01]),
      at: DateTime.utc(2026, 1, 2, 3, 4, 5, 0, 6),
      day: DateTime.utc(2026, 1, 2),
      secs: DateTime.utc(2026, 1, 2, 3, 4, 5),
      vis: Visibility.publicView,
      visNum: Visibility.publicView,
      meta: Meta(owner: 'a@b.io'),
      flat: Meta(owner: 'c@d.io'),
      labels: {'x': 'y'},
      rawJson: {'free': [1, true]},
      body: NoteBodyN(42),
    );

Future<void> main(List<String> args) async {
  final client = NotesClient(args[0]);
  final wide = Wide(
    nums: [-9007199254740993, 0, 9007199254740993],
    ids: [BigInt.parse('18446744073709551615'), BigInt.one],
    byName: {'a': -1, 'b': 9007199254740991},
    stamps: [DateTime.utc(2026, 1, 2, 3, 4, 5, 0, 6), DateTime.utc(1999, 12, 31, 23, 59, 59)],
    blobs: [Uint8List.fromList([1]), Uint8List.fromList([2, 3])],
    levels: [Visibility.publicView, Visibility.private],
    metas: [Meta(owner: 'x'), Meta(owner: 'y')],
    metaMap: {'k': Meta(owner: 'z')},
    optNum: -9007199254740993,
    flag: true,
    optFlag: false,
    small: -7,
    f32: 0.5,
  );
  final wideEcho = await client.echoWide(wide);
  if (wideEcho != wide) fail('wide mismatch:\n  sent $wide\n  got  $wideEcho');
  final note = sample('n-1')..maybe = 'set';
  final echoed = await client.create(note);
  if (echoed != note) fail('echo mismatch:\n  sent $note\n  got  $echoed');
  final fetched = await client.fetch(Fetch(id: 'n-1', limit: 5, tags: ['p', 'q']));
  if (fetched != sample('n-1|5|p,q')) fail('fetch mismatch: $fetched');
  try {
    await client.fetch(Fetch(id: 'missing', limit: 1));
    fail('expected NotFound');
  } on NotFound catch (e) {
    if (e.code != 'gone') fail('typed error: $e');
  }
  final ids = <String>[];
  try {
    await for (final event in client.watch(Fetch(id: 'w', limit: 1))) {
      if (event != sample(event.id)) fail('stream event mismatch: $event');
      ids.add(event.id);
    }
    fail('expected stream error');
  } on StreamException catch (e) {
    if ((e.payload as Map)['code'] != 'done') fail('stream error payload: ${e.payload}');
  }
  if (ids.join(',') != 'one,two') fail('stream events: $ids');
  client.close();
  print('OK');
}
`

func compileHTTPWireSchema(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onktest.Parse(httpWireSchema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "notes.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg.Files[0]
}

func buildHTTPGoServer(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-go-server", buildHTTPGoServerIn)
}

func buildHTTPGoServerIn(t *testing.T, goDir string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	file := compileHTTPWireSchema(t)
	for name, generate := range map[string]func(*onkir.File) ([]byte, error){
		"notes/server.go":       gengo.GenerateServer,
		"notes/types.gen.go":    gengo.GenerateTypes,
		"notes/validate.gen.go": gengo.GenerateValidation,
	} {
		src, err := generate(file)
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		writeFile(t, filepath.Join(goDir, name), string(src))
	}
	writeFile(t, filepath.Join(goDir, "go.mod"), "module interop\n\ngo 1.24\n")
	writeFile(t, filepath.Join(goDir, "main.go"), goHTTPHarnessMain)
	bin := filepath.Join(goDir, "server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(t, goDir, "go", "build", "-o", bin, ".")
	return bin
}
