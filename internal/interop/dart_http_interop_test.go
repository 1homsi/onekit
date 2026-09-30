package interop

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const httpWireSchema = `
package notes

enum Visibility { PRIVATE PUBLIC_VIEW }
message Meta { owner: string }
message Note {
  id: string
  count: int32
  big: int64
  huge: uint64
  ratio: float64
  tags: string[]
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
  labels: map[string, string]
  raw_json: json
  body: oneof(discriminator: "kind") {
    text: string @tag("text")
    meta_ref: Meta @tag("meta")
    n: int64 @tag("n")
  }
}
message Wide {
  nums: int64[]
  ids: uint64[]
  by_name: map[string, int64]
  stamps: timestamp[]
  blobs: bytes[]
  levels: Visibility[]
  metas: Meta[]
  meta_map: map[string, Meta]
  opt_num: int64?
  flag: bool
  opt_flag: bool?
  small: int32
  f32: float32
  zero_str: string
}
message Fetch { id: string limit: int32? @query tags: string[] @query }
message NotFound @status(404) { code: string }

service Notes {
  create(Note) -> Note @post("/notes")
  echoWide(Wide) -> Wide @post("/wide")
  fetch(Fetch) -> Note | NotFound @get("/notes/{id}")
  watch(Fetch) -> Note | NotFound @get("/watch/{id}") @stream
}
`

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
	ast, err := onklang.Parse(httpWireSchema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "notes.onk", AST: ast}})
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
