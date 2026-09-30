package interop

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/1homsi/onekit/internal/gendart"
	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/genswift"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkir"
)

const goHTTPClientHarness = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"interop/notes"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func sample(id string) *notes.Note {
	return &notes.Note{
		Id: id, Count: 3, Big: -9007199254740993, Huge: 18446744073709551615, Ratio: 1.5,
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

func wideSample() *notes.Wide {
	optNum := int64(-9007199254740993)
	optFlag := false
	return &notes.Wide{
		Nums:    []int64{-9007199254740993, 0, 9007199254740993},
		Ids:     []uint64{18446744073709551615, 1},
		ByName:  map[string]int64{"a": -1, "b": 9007199254740991},
		Stamps:  []time.Time{time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC), time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)},
		Blobs:   [][]byte{{1}, {2, 3}},
		Levels:  []notes.Visibility{notes.VisibilityPublicView, notes.VisibilityPrivate},
		Metas:   []*notes.Meta{{Owner: "x"}, {Owner: "y"}},
		MetaMap: map[string]*notes.Meta{"k": {Owner: "z"}},
		OptNum:  &optNum,
		Flag:    true,
		OptFlag: &optFlag,
		Small:   -7,
		F32:     0.5,
	}
}

func same(what string, got, want *notes.Note) {
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		fail("%s mismatch:\n  got  %s\n  want %s", what, g, w)
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := notes.NewNotesClient(os.Args[1])

	note := sample("n-1")
	maybe := "set"
	note.Maybe = &maybe
	echoed, err := client.Create(ctx, note)
	if err != nil {
		fail("create: %v", err)
	}
	same("echo", echoed, note)

	wide := wideSample()
	wideEcho, err := client.EchoWide(ctx, wide)
	if err != nil {
		fail("echoWide: %v", err)
	}
	wg, _ := json.Marshal(wideEcho)
	ww, _ := json.Marshal(wide)
	if string(wg) != string(ww) {
		fail("wide mismatch:\n  got  %s\n  want %s", wg, ww)
	}

	limit := int32(5)
	fetched, err := client.Fetch(ctx, &notes.Fetch{Id: "n-1", Limit: &limit, Tags: []string{"p", "q"}})
	if err != nil {
		fail("fetch: %v", err)
	}
	expected, err := client.Create(ctx, sample("n-1|5|p,q"))
	if err != nil {
		fail("create expected: %v", err)
	}
	same("fetch", fetched, expected)

	one := int32(1)
	_, err = client.Fetch(ctx, &notes.Fetch{Id: "missing", Limit: &one})
	var notFound *notes.NotFound
	if !errors.As(err, &notFound) || notFound.Code != "gone" {
		fail("typed error: %v", err)
	}

	stream, err := client.Watch(ctx, &notes.Fetch{Id: "w", Limit: &one})
	if err != nil {
		fail("watch: %v", err)
	}
	defer stream.Close()
	var ids []string
	var event notes.Note
	for stream.Next(&event) {
		want, err := client.Create(ctx, sample(event.Id))
		if err != nil {
			fail("create stream expected: %v", err)
		}
		same("stream event", &event, want)
		ids = append(ids, event.Id)
		event = notes.Note{}
	}
	if fmt.Sprint(ids) != "[one two]" {
		fail("stream events: %v", ids)
	}
	if stream.Err() == nil || !strings.Contains(stream.Err().Error(), ` + "`" + `"code":"done"` + "`" + `) {
		fail("stream error: %v", stream.Err())
	}
	fmt.Println("OK")
}
`

const tsHTTPClientHarness = `import { strict as assert } from "node:assert";
import { NotesClient, TypedApiError } from "./client.js";
import type { Note, Wide } from "./types.js";

function sample(id: string): Note {
  return {
    id, count: 3, big: "-9007199254740993", huge: "18446744073709551615", ratio: 1.5,
    tags: ["a", "b"], data: "AQID", hex: "ff01", at: "2026-01-02T03:04:05.000006Z",
    day: "2026-01-02", secs: 1767323045, vis: "PUBLIC_VIEW", visNum: 1,
    meta: { owner: "a@b.io" }, mOwner: "c@d.io", labels: { x: "y" },
    rawJson: { free: [1, true] }, body: { kind: "n", n: "42" },
  };
}

function plain(value: unknown): unknown {
  return JSON.parse(JSON.stringify(value), (_key, item) => (item === "" ? undefined : item));
}

function same(what: string, got: Note, want: Note): void {
  try {
    assert.deepEqual(plain(got), plain(want));
  } catch (err) {
    console.error(what + " mismatch: " + (err as Error).message);
    process.exit(1);
  }
}

function wideSample(): Wide {
  return {
    nums: ["-9007199254740993", "0", "9007199254740993"],
    ids: ["18446744073709551615", "1"],
    byName: { a: -1, b: 9007199254740991 },
    stamps: ["2026-01-02T03:04:05.000006Z", "1999-12-31T23:59:59Z"],
    blobs: ["AQ==", "AgM="],
    levels: ["PUBLIC_VIEW", "PRIVATE"],
    metas: [{ owner: "x" }, { owner: "y" }],
    metaMap: { k: { owner: "z" } },
    optNum: "-9007199254740993",
    flag: true,
    optFlag: false,
    small: -7,
    f32: 0.5,
  };
}

async function main(): Promise<void> {
  const client = new NotesClient(process.argv[2]!);
  const wide = wideSample();
  const wideEcho = await client.echoWide(wide);
  try {
    assert.deepEqual(plain(wideEcho), plain(wide));
  } catch (err) {
    console.error("wide mismatch: " + (err as Error).message);
    process.exit(1);
  }
  const note: Note = { ...sample("n-1"), maybe: "set" };
  same("echo", await client.create(note), note);

  const fetched = await client.fetch({ id: "n-1", limit: 5, tags: ["p", "q"] });
  same("fetch", fetched, await client.create(sample("n-1|5|p,q")));

  let typed: unknown;
  try {
    await client.fetch({ id: "missing", limit: 1 });
  } catch (err) {
    typed = err;
  }
  if (!(typed instanceof TypedApiError) || typed.errorType !== "NotFound" || (typed.data as { code?: string }).code !== "gone") {
    console.error("typed error: " + String(typed));
    process.exit(1);
  }

  const ids: string[] = [];
  let streamError: unknown;
  try {
    for await (const event of client.watch({ id: "w", limit: 1 })) {
      same("stream event", event, await client.create(sample(event.id!)));
      ids.push(event.id!);
    }
  } catch (err) {
    streamError = err;
  }
  if (ids.join(",") !== "one,two") {
    console.error("stream events: " + ids.join(","));
    process.exit(1);
  }
  if (!String(streamError).includes("done")) {
    console.error("stream error: " + String(streamError));
    process.exit(1);
  }
  console.log("OK");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`

const pyHTTPClientHarness = `import sys

from client import NotesClient, StreamError
from models import Fetch, Meta, Note, NoteBodyN, NotFound, Visibility, Wide


def fail(message):
    print(message, file=sys.stderr)
    sys.exit(1)


def sample(note_id):
    return Note(
        id=note_id, count=3, big=-9007199254740993, huge=18446744073709551615, ratio=1.5,
        tags=["a", "b"], data=b"\x01\x02\x03", hex=b"\xff\x01",
        at="2026-01-02T03:04:05.000006Z", day="2026-01-02", secs=1767323045,
        vis=Visibility.PUBLIC_VIEW, vis_num=Visibility.PUBLIC_VIEW,
        meta=Meta(owner="a@b.io"), flat=Meta(owner="c@d.io"), labels={"x": "y"},
        raw_json={"free": [1, True]}, body=NoteBodyN(n=42),
    )


client = NotesClient(sys.argv[1])
wide = Wide(
    nums=[-9007199254740993, 0, 9007199254740993], ids=[18446744073709551615, 1],
    by_name={"a": -1, "b": 9007199254740991},
    stamps=["2026-01-02T03:04:05.000006Z", "1999-12-31T23:59:59Z"],
    blobs=[b"\x01", b"\x02\x03"], levels=[Visibility.PUBLIC_VIEW, Visibility.PRIVATE],
    metas=[Meta(owner="x"), Meta(owner="y")], meta_map={"k": Meta(owner="z")},
    opt_num=-9007199254740993, flag=True, opt_flag=False, small=-7, f32=0.5,
)
wide_echo = client.echo_wide(wide)
if wide_echo != wide:
    fail("wide mismatch:\n  sent %r\n  got  %r" % (wide, wide_echo))

note = sample("n-1")
note.maybe = "set"
echoed = client.create(note)
if echoed != note:
    fail("echo mismatch:\n  sent %r\n  got  %r" % (note, echoed))

fetched = client.fetch(Fetch(id="n-1", limit=5, tags=["p", "q"]))
expected = client.create(sample("n-1|5|p,q"))
if fetched != expected:
    fail("fetch mismatch:\n  got  %r\n  want %r" % (fetched, expected))

try:
    client.fetch(Fetch(id="missing", limit=1))
    fail("expected NotFound")
except NotFound as err:
    if err.code != "gone":
        fail("typed error: %r" % (err,))

ids = []
try:
    for event in client.watch(Fetch(id="w", limit=1)):
        if event != client.create(sample(event.id)):
            fail("stream event mismatch: %r" % (event,))
        ids.append(event.id)
    fail("expected stream error")
except StreamError as err:
    if err.payload.get("code") != "done":
        fail("stream error payload: %r" % (err.payload,))
if ids != ["one", "two"]:
    fail("stream events: %r" % (ids,))
print("OK")
`

func buildHTTPGoClient(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-go-client", buildHTTPGoClientIn)
}

func buildHTTPGoClientIn(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	file := compileHTTPWireSchema(t)
	for name, generate := range map[string]func(*onkir.File) ([]byte, error){
		"notes/client.go":       gengo.GenerateClient,
		"notes/types.gen.go":    gengo.GenerateTypes,
		"notes/validate.gen.go": gengo.GenerateValidation,
	} {
		src, err := generate(file)
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		writeFile(t, filepath.Join(dir, name), string(src))
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module interop\n\ngo 1.24\n")
	writeFile(t, filepath.Join(dir, "main.go"), goHTTPClientHarness)
	bin := filepath.Join(dir, "client")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(t, dir, "go", "build", "-o", bin, ".")
	return bin
}

func buildHTTPTSClient(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-ts-client", buildHTTPTSClientIn)
}

func buildHTTPTSClientIn(t *testing.T, dir string) string {
	t.Helper()
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	file := compileHTTPWireSchema(t)
	writeFile(t, filepath.Join(dir, "types.ts"), string(gents.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "client.ts"), string(gents.GenerateClientWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "harness.ts"), tsHTTPClientHarness)
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "http-interop", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "node16",
    "moduleResolution": "node16",
    "esModuleInterop": true,
    "strict": true,
    "types": ["node"],
    "lib": ["ES2022", "DOM"]
  },
  "files": ["types.ts", "client.ts", "harness.ts"]
}
`)
	run(t, dir, "npm", "install", "--no-audit", "--no-fund", "@types/node")
	run(t, dir, "tsc", "-p", "tsconfig.json")
	return dir
}

const rustHTTPCargoToml = `[package]
name = "http-interop"
version = "0.1.0"
edition = "2024"

[dependencies]
async-stream = "0.3"
base64 = "0.22"
futures-util = "0.3"
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
tokio = { version = "1", features = ["full"] }
urlencoding = "2"
`

const rustHTTPClientHarness = `#![allow(dead_code)]
mod generated;

use futures_util::StreamExt;
use generated::client::*;
use generated::types::*;
use std::collections::HashMap;

fn fail(message: String) -> ! {
    eprintln!("{message}");
    std::process::exit(1);
}

fn sample(id: &str) -> Note {
    Note {
        id: id.into(),
        count: 3,
        big: -9007199254740993,
        huge: 18446744073709551615,
        ratio: 1.5,
        tags: vec!["a".into(), "b".into()],
        data: vec![1, 2, 3],
        hex: vec![0xff, 0x01],
        at: "2026-01-02T03:04:05.000006Z".into(),
        day: "2026-01-02".into(),
        secs: 1767323045,
        vis: Visibility::PublicView,
        vis_num: Visibility::PublicView,
        maybe: None,
        meta: Some(Box::new(Meta { owner: "a@b.io".into() })),
        flat: Some(Box::new(Meta { owner: "c@d.io".into() })),
        labels: HashMap::from([("x".to_string(), "y".to_string())]),
        raw_json: serde_json::json!({"free": [1, true]}),
        body: Some(NoteBody::N(42)),
    }
}

fn wide_sample() -> Wide {
    Wide {
        nums: vec![-9007199254740993, 0, 9007199254740993],
        ids: vec![18446744073709551615, 1],
        by_name: HashMap::from([("a".to_string(), -1), ("b".to_string(), 9007199254740991)]),
        stamps: vec!["2026-01-02T03:04:05.000006Z".into(), "1999-12-31T23:59:59Z".into()],
        blobs: vec![vec![1], vec![2, 3]],
        levels: vec![Visibility::PublicView, Visibility::Private],
        metas: vec![Meta { owner: "x".into() }, Meta { owner: "y".into() }],
        meta_map: HashMap::from([("k".to_string(), Meta { owner: "z".into() })]),
        opt_num: Some(-9007199254740993),
        flag: true,
        opt_flag: Some(false),
        small: -7,
        f32: 0.5,
        zero_str: String::new(),
    }
}

#[tokio::main]
async fn main() {
    let base = std::env::args().nth(1).unwrap_or_else(|| fail("base url required".into()));
    let client = NotesClient::new(base);

    let wide = wide_sample();
    let wide_echo = client.echo_wide(&wide).await.unwrap_or_else(|e| fail(format!("echo_wide: {e}")));
    if wide_echo != wide {
        fail(format!("wide mismatch:\n  sent {wide:?}\n  got  {wide_echo:?}"));
    }

    let mut note = sample("n-1");
    note.maybe = Some("set".into());
    let echoed = client.create(&note).await.unwrap_or_else(|e| fail(format!("create: {e}")));
    if echoed != note {
        fail(format!("echo mismatch:\n  sent {note:?}\n  got  {echoed:?}"));
    }

    let fetched = client
        .fetch(&Fetch { id: "n-1".into(), limit: Some(5), tags: vec!["p".into(), "q".into()] })
        .await
        .unwrap_or_else(|e| fail(format!("fetch: {e}")));
    let expected = client.create(&sample("n-1|5|p,q")).await.unwrap_or_else(|e| fail(format!("create expected: {e}")));
    if fetched != expected {
        fail(format!("fetch mismatch:\n  got  {fetched:?}\n  want {expected:?}"));
    }

    match client.fetch(&Fetch { id: "missing".into(), limit: Some(1), tags: vec![] }).await {
        Err(NotesFetchError::NotFound(error)) if error.code == "gone" => {}
        other => fail(format!("typed error: {other:?}")),
    }

    let watch_request = Fetch { id: "w".into(), limit: Some(1), tags: vec![] };
    let stream = client
        .watch(&watch_request)
        .await
        .unwrap_or_else(|e| fail(format!("watch: {e}")));
    let mut stream = Box::pin(stream);
    let mut ids = Vec::new();
    let mut stream_error = None;
    while let Some(item) = stream.next().await {
        match item {
            Ok(event) => {
                let want = client.create(&sample(&event.id)).await.unwrap_or_else(|e| fail(format!("create stream expected: {e}")));
                if event != want {
                    fail(format!("stream event mismatch: {event:?}"));
                }
                ids.push(event.id);
            }
            Err(error) => {
                stream_error = Some(format!("{error:?}"));
                break;
            }
        }
    }
    if ids.join(",") != "one,two" {
        fail(format!("stream events: {ids:?}"));
    }
    if !stream_error.as_deref().unwrap_or("").contains("done") {
        fail(format!("stream error: {stream_error:?}"));
    }
    println!("OK");
}
`

func buildHTTPRustClient(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-rust-client", buildHTTPRustClientIn)
}

func buildHTTPRustClientIn(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	file := compileHTTPWireSchema(t)
	writeFile(t, filepath.Join(dir, "Cargo.toml"), rustHTTPCargoToml)
	writeFile(t, filepath.Join(dir, "src", "main.rs"), rustHTTPClientHarness)
	writeFile(t, filepath.Join(dir, "src", "generated", "mod.rs"), "pub mod types;\npub mod client;\n")
	writeFile(t, filepath.Join(dir, "src", "generated", "types.rs"), string(genrust.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "src", "generated", "client.rs"), string(genrust.GenerateClient(file)))
	run(t, dir, "cargo", "build", "--quiet")
	bin := filepath.Join(cargoTargetDir(), "debug", "http-interop")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}

const tsHTTPServerHarness = `import http from "node:http";
import type { AddressInfo } from "node:net";
import { attachNotesNodeHandlers, httpErrorFromNotFound, type NotesHandler } from "./server.js";
import type { Note } from "./types.js";

function sample(id: string): Note {
  return {
    id, count: 3, big: "-9007199254740993", huge: "18446744073709551615", ratio: 1.5,
    tags: ["a", "b"], data: "AQID", hex: "ff01", at: "2026-01-02T03:04:05.000006Z",
    day: "2026-01-02", secs: 1767323045, vis: "PUBLIC_VIEW", visNum: 1,
    meta: { owner: "a@b.io" }, mOwner: "c@d.io", labels: { x: "y" },
    rawJson: { free: [1, true] }, body: { kind: "n", n: "42" },
  };
}

const handler: NotesHandler = {
  async create(req) {
    return req;
  },
  async echoWide(req) {
    return req;
  },
  async fetch(req) {
    if (req.id === "missing") throw httpErrorFromNotFound({ code: "gone" });
    return sample(req.id + "|" + req.limit + "|" + (req.tags ?? []).join(","));
  },
  watch() {
    let step = 0;
    return new ReadableStream<Note>({
      pull(controller) {
        step += 1;
        if (step === 1) controller.enqueue(sample("one"));
        else if (step === 2) controller.enqueue(sample("two"));
        else throw httpErrorFromNotFound({ code: "done" });
      },
    });
  },
};

const server = http.createServer();
attachNotesNodeHandlers(server, handler);
server.listen(0, "127.0.0.1", () => {
  console.log("PORT=" + (server.address() as AddressInfo).port);
});
`

const rustHTTPServerCargoToml = `[package]
name = "http-server-interop"
version = "0.1.0"
edition = "2024"

[dependencies]
axum = "0.8"
base64 = "0.22"
futures-util = "0.3"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
tokio = { version = "1", features = ["full"] }
urlencoding = "2"
validator = { version = "0.20", features = ["derive"] }
`

const rustHTTPServerHarness = `#![allow(dead_code)]
mod generated;

use futures_util::{Stream, stream};
use generated::server::*;
use generated::types::*;
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;

fn sample(id: &str) -> Note {
    Note {
        id: id.into(),
        count: 3,
        big: -9007199254740993,
        huge: 18446744073709551615,
        ratio: 1.5,
        tags: vec!["a".into(), "b".into()],
        data: vec![1, 2, 3],
        hex: vec![0xff, 0x01],
        at: "2026-01-02T03:04:05.000006Z".into(),
        day: "2026-01-02".into(),
        secs: 1767323045,
        vis: Visibility::PublicView,
        vis_num: Visibility::PublicView,
        maybe: None,
        meta: Some(Box::new(Meta { owner: "a@b.io".into() })),
        flat: Some(Box::new(Meta { owner: "c@d.io".into() })),
        labels: HashMap::from([("x".to_string(), "y".to_string())]),
        raw_json: serde_json::json!({"free": [1, true]}),
        body: Some(NoteBody::N(42)),
    }
}

struct Impl;

impl Notes for Impl {
    async fn create(&self, _context: RequestContext, req: Note) -> Result<Note, NotesCreateServerError> {
        Ok(req)
    }

    async fn echo_wide(&self, _context: RequestContext, req: Wide) -> Result<Wide, NotesEchoWideServerError> {
        Ok(req)
    }

    async fn fetch(&self, _context: RequestContext, req: Fetch) -> Result<Note, NotesFetchServerError> {
        if req.id == "missing" {
            return Err(NotesFetchServerError::NotFound(NotFound { code: "gone".into() }));
        }
        Ok(sample(&format!("{}|{}|{}", req.id, req.limit.unwrap_or(0), req.tags.join(","))))
    }

    async fn watch(
        &self,
        _context: RequestContext,
        _req: Fetch,
    ) -> Result<Pin<Box<dyn Stream<Item = Result<Note, NotesWatchServerError>> + Send>>, NotesWatchServerError> {
        let items: Vec<Result<Note, NotesWatchServerError>> = vec![
            Ok(sample("one")),
            Ok(sample("two")),
            Err(NotesWatchServerError::NotFound(NotFound { code: "done".into() })),
        ];
        Ok(Box::pin(stream::iter(items)))
    }
}

#[tokio::main]
async fn main() {
    let app = notes_router(Arc::new(Impl));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.expect("bind");
    println!("PORT={}", listener.local_addr().expect("addr").port());
    axum::serve(listener, app).await.expect("serve");
}
`

func buildHTTPTSServer(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-ts-server", buildHTTPTSServerIn)
}

func buildHTTPTSServerIn(t *testing.T, dir string) string {
	t.Helper()
	for _, tool := range []string{"tsc", "npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not available")
		}
	}
	file := compileHTTPWireSchema(t)
	writeFile(t, filepath.Join(dir, "types.ts"), string(gents.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "server.ts"), string(gents.GenerateServerWithResolver(file, nil)))
	writeFile(t, filepath.Join(dir, "harness.ts"), tsHTTPServerHarness)
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "http-server-interop", "private": true}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "node16",
    "moduleResolution": "node16",
    "esModuleInterop": true,
    "strict": true,
    "types": ["node"],
    "lib": ["ES2022", "DOM"]
  },
  "files": ["types.ts", "server.ts", "harness.ts"]
}
`)
	run(t, dir, "npm", "install", "--no-audit", "--no-fund", "@types/node")
	run(t, dir, "tsc", "-p", "tsconfig.json")
	return dir
}

func buildHTTPRustServer(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-rust-server", buildHTTPRustServerIn)
}

func buildHTTPRustServerIn(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	file := compileHTTPWireSchema(t)
	writeFile(t, filepath.Join(dir, "Cargo.toml"), rustHTTPServerCargoToml)
	writeFile(t, filepath.Join(dir, "src", "main.rs"), rustHTTPServerHarness)
	writeFile(t, filepath.Join(dir, "src", "generated", "mod.rs"), "pub mod types;\npub mod server;\n")
	writeFile(t, filepath.Join(dir, "src", "generated", "types.rs"), string(genrust.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "src", "generated", "server.rs"), string(genrust.GenerateServer(file)))
	run(t, dir, "cargo", "build", "--quiet")
	bin := filepath.Join(cargoTargetDir(), "debug", "http-server-interop")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}

type httpContractServer struct {
	name  string
	start func(t *testing.T) string
}

type httpContractClient struct {
	name string
	run  func(t *testing.T, baseURL string)
}

func httpContractServers() []httpContractServer {
	return []httpContractServer{
		{"go", func(t *testing.T) string { return startServer(t, "", buildHTTPGoServer(t)) }},
		{"ts", func(t *testing.T) string {
			return startServer(t, buildHTTPTSServer(t), "node", "harness.js")
		}},
		{"rust", func(t *testing.T) string { return startServer(t, "", buildHTTPRustServer(t)) }},
	}
}

func httpContractClients() []httpContractClient {
	return []httpContractClient{
		{"go", func(t *testing.T, baseURL string) { expectOK(t, "", buildHTTPGoClient(t), baseURL) }},
		{"ts", func(t *testing.T, baseURL string) { expectOK(t, buildHTTPTSClient(t), "node", "harness.js", baseURL) }},
		{"python", func(t *testing.T, baseURL string) {
			python, err := exec.LookPath("python3")
			if err != nil {
				if python, err = exec.LookPath("python"); err != nil {
					t.Skip("python not available")
				}
			}
			file := compileHTTPWireSchema(t)
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "models.py"), string(genpy.GenerateTypes(file)))
			writeFile(t, filepath.Join(dir, "client.py"), string(genpy.GenerateClient(file, "models")))
			writeFile(t, filepath.Join(dir, "harness.py"), pyHTTPClientHarness)
			expectOK(t, dir, python, "harness.py", baseURL)
		}},
		{"rust", func(t *testing.T, baseURL string) { expectOK(t, "", buildHTTPRustClient(t), baseURL) }},
		{"swift", func(t *testing.T, baseURL string) { expectOK(t, "", buildHTTPSwiftClient(t), baseURL) }},
		{"dart", func(t *testing.T, baseURL string) {
			if _, err := exec.LookPath("dart"); err != nil {
				t.Skip("dart not available")
			}
			file := compileHTTPWireSchema(t)
			dir := t.TempDir()
			lib := filepath.Join(dir, "lib")
			writeFile(t, filepath.Join(dir, "pubspec.yaml"), dartPubspec)
			writeFile(t, filepath.Join(lib, "onekit.dart"), string(gendart.GenerateRuntime()))
			writeFile(t, filepath.Join(lib, "models.dart"), string(gendart.GenerateTypes(file)))
			writeFile(t, filepath.Join(lib, "client.dart"), string(gendart.GenerateClient(file)))
			writeFile(t, filepath.Join(dir, "bin", "harness.dart"), dartHTTPHarness)
			run(t, dir, "dart", "pub", "get")
			expectOK(t, dir, "dart", "run", "bin/harness.dart", baseURL)
		}},
	}
}

func TestHTTPContractMatrix(t *testing.T) {
	for _, server := range httpContractServers() {
		t.Run("server_"+server.name, func(t *testing.T) {
			for _, client := range httpContractClients() {
				t.Run("client_"+client.name, func(t *testing.T) {
					port := server.start(t)
					client.run(t, "http://127.0.0.1:"+port)
				})
			}
		})
	}
}

const swiftHTTPClientHarness = `import Foundation

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

func sample(_ id: String) -> Note {
    Note(
        id: id, count: 3, big: -9007199254740993, huge: 18446744073709551615, ratio: 1.5,
        tags: ["a", "b"], data: Data([1, 2, 3]), hex: Data([0xff, 0x01]),
        at: Date(timeIntervalSince1970: 1767323045.000006),
        day: Date(timeIntervalSince1970: 1767312000),
        secs: Date(timeIntervalSince1970: 1767323045),
        vis: .publicView, visNum: .publicView,
        meta: Meta(owner: "a@b.io"), flat: Meta(owner: "c@d.io"), labels: ["x": "y"],
        rawJson: ["free": [1, true] as [Any]], body: .n(42)
    )
}

let client = NotesClient(CommandLine.arguments[1])

let wide = Wide(
    nums: [-9007199254740993, 0, 9007199254740993], ids: [18446744073709551615, 1],
    byName: ["a": -1, "b": 9007199254740991],
    stamps: [Date(timeIntervalSince1970: 1767323045.000006), Date(timeIntervalSince1970: 946684799)],
    blobs: [Data([1]), Data([2, 3])], levels: [.publicView, .private_],
    metas: [Meta(owner: "x"), Meta(owner: "y")], metaMap: ["k": Meta(owner: "z")],
    optNum: -9007199254740993, flag: true, optFlag: false, small: -7, f32: 0.5
)
let wideEcho = try await client.echoWide(wide)
if wideEcho != wide { fail("wide mismatch:\n  sent \(wide)\n  got  \(wideEcho)") }

let note = sample("n-1")
note.maybe = "set"
let echoed = try await client.create(note)
if echoed != note { fail("echo mismatch:\n  sent \(note)\n  got  \(echoed)") }

let fetched = try await client.fetch(Fetch(id: "n-1", limit: 5, tags: ["p", "q"]))
let expected = try await client.create(sample("n-1|5|p,q"))
if fetched != expected { fail("fetch mismatch:\n  got  \(fetched)\n  want \(expected)") }

do {
    _ = try await client.fetch(Fetch(id: "missing", limit: 1))
    fail("expected NotFound")
} catch let error as NotFound {
    if error.code != "gone" { fail("typed error: \(error)") }
}

var ids: [String] = []
do {
    for try await event in client.watch(Fetch(id: "w", limit: 1)) {
        let want = try await client.create(sample(event.id))
        if event != want { fail("stream event mismatch: \(event)") }
        ids.append(event.id)
    }
    fail("expected stream error")
} catch OnekitError.stream(let payload) {
    let code = (payload as? [String: Any])?["code"] as? String
    if code != "done" { fail("stream error payload: \(String(describing: payload))") }
}
if ids != ["one", "two"] { fail("stream events: \(ids)") }
print("OK")
`

func buildHTTPSwiftClient(t *testing.T) string {
	t.Helper()
	return cachedHarness(t, "http-swift-client", buildHTTPSwiftClientIn)
}

func buildHTTPSwiftClientIn(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	file := compileHTTPWireSchema(t)
	writeFile(t, filepath.Join(dir, "Onekit.swift"), string(genswift.GenerateRuntime()))
	writeFile(t, filepath.Join(dir, "Models.swift"), string(genswift.GenerateTypes(file)))
	writeFile(t, filepath.Join(dir, "Client.swift"), string(genswift.GenerateClient(file)))
	writeFile(t, filepath.Join(dir, "main.swift"), swiftHTTPClientHarness)
	bin := filepath.Join(dir, "harness")
	run(t, dir, "swiftc", "-o", bin, "Onekit.swift", "Models.swift", "Client.swift", "main.swift")
	return bin
}
