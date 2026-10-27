package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
	"github.com/1homsi/onekit/internal/onktest"
)

func parseSrc(src string) (*onkschema.File, error) { return onktest.Parse(src) }

func TestWSMethodCompiles(t *testing.T) {
	ast, err := parseSrc(`namespace w;

model Msg {
  room: string;
  text: string;
}

model Ev {
  @encode(string)
  seq: int64;
}

@route("/v1")
interface S {
  @Onkt.ws
  @route("/rooms/{room}")
  chat(...Msg): Ev;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "w.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	m := pkg.Files[0].Services[0].Methods[0]
	path, _ := m.WebSocketPath()
	if !m.IsWebSocket() || path != "/rooms/{room}" {
		t.Fatalf("ws method not recognized: %+v", m)
	}
}

func TestWSIDCorrelation(t *testing.T) {
	ast, err := parseSrc(`namespace w;

model RunRequest {
  code: string;
}

model RunResult {
  exit_code: int32;
}

model HostCall {
  @Onkt.wsId
  id: string;
  method: string;
}

model HostResult {
  @Onkt.wsId
  id: string;
  value: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("run") run: RunRequest,
  @Onkt.tag("host_call") host_call: HostCall,
  @Onkt.tag("host_result") host_result: HostResult,
  @Onkt.tag("run_result") run_result: RunResult,
}

model Frame {
  payload: FramePayload;
}

@route("/v1")
interface Runtime {
  @Onkt.ws
  @route("/execute")
  execute(...Frame): Frame;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "w.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("expected @ws_id fields on distinct oneof variants to compile, got: %v", err)
	}
	frame := pkg.Files[0].Messages[0]
	if frame.Name != "Frame" {
		for _, m := range pkg.Files[0].Messages {
			if m.Name == "Frame" {
				frame = m
			}
		}
	}
	payload := frame.Fields[0]
	if payload.Oneof == nil {
		t.Fatalf("expected payload to be a oneof")
	}
	found := false
	for _, variant := range payload.Oneof.Variants {
		if variant.Name != "host_call" {
			continue
		}
		for _, f := range variant.Type.Message.Fields {
			if f.HasDecorator("ws_id") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected host_call variant's id field to carry @ws_id")
	}
}

func TestWSIDRejectsInvalidUsage(t *testing.T) {
	cases := []struct{ src, want string }{
		{
			`
model M {
  @Onkt.wsId
  active: boolean;
}

model E {
  x: int32;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...M): E;
}
`,
			"@ws_id requires a non-repeated string or integer field",
		},
		{
			`
model M {
  @Onkt.wsId
  ids: string[];
}

model E {
  x: int32;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...M): E;
}
`,
			"@ws_id requires a non-repeated string or integer field",
		},
		{
			`
model M {
  @Onkt.wsId
  a: string;
  @Onkt.wsId
  b: string;
}

model E {
  x: int32;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...M): E;
}
`,
			"has more than one @ws_id field",
		},
		{
			`
model M {
  text: string;
}

model A {
  @Onkt.wsId
  id: string;
}

model B {
  @Onkt.wsId
  @encode(string)
  id: int64;
}

@Onkt.discriminator("type")
union UPayload {
  @Onkt.tag("a") a: A,
  @Onkt.tag("b") b: B,
}

model U {
  payload: UPayload;
}

model E {
  x: int32;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...U): E;
}
`,
			"must share one scalar type",
		},
	}
	for _, tc := range cases {
		src := `namespace w;
` + tc.src
		ast, err := parseSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.want, err)
		}
		if _, err := Compile([]Source{{Path: "w.tsp", AST: ast}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q got %v", tc.want, err)
		}
	}
}

func TestWSRejectsConflicts(t *testing.T) {
	cases := []struct{ src, want string }{
		{`
interface S {
  @Onkt.ws
  @route("/y")
  @Onkt.stream
  f(...M): E;
}
`, "cannot be combined with @stream"},
		{`
model B {
  @body text: string;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...B): E;
}
`, "@ws does not support @body"},
		{`
interface S {
  @Onkt.ws
  f(...M): E;
}
`, "@ws requires one non-empty route"},
	}
	for _, tc := range cases {
		src := `namespace w;

model M {
  text: string;
}

model E {
  x: int32;
}
` + tc.src
		ast, err := parseSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.want, err)
		}
		if _, err := Compile([]Source{{Path: "w.tsp", AST: ast}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q got %v", tc.want, err)
		}
	}
}

// A TypeSpec operation has one route decorator; a document that gives a
// method both an HTTP verb and @ws is refused.
func TestWSRejectsAnHTTPVerbInDocuments(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	file := onktest.File("w",
		onktest.Message("M", onktest.Field("text", "string")),
		onktest.Message("E", onktest.Field("x", "int32")),
		onktest.Service("S", "", onktest.RPC("f", "M", "E", dec("get", q("/x")), dec("ws", q("/y")))),
	)
	if _, err := Compile([]Source{{Path: "w.tsp", AST: file}}); err == nil || !strings.Contains(err.Error(), "@ws cannot be combined with an HTTP verb") {
		t.Fatalf("got %v", err)
	}
}

func TestWSCancelVariant(t *testing.T) {
	const valid = `namespace w;

model Call {
  @Onkt.wsId
  id: string;
}

model Cancel {
  @Onkt.wsId
  id: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("call") call: Call,
  @Onkt.tag("cancel") @Onkt.wsCancel cancel: Cancel,
}

model Frame {
  payload: FramePayload;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Frame): Frame;
}
`
	ast, err := parseSrc(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile([]Source{{Path: "w.tsp", AST: ast}}); err != nil {
		t.Fatalf("expected a @ws_cancel variant carrying @ws_id to compile, got: %v", err)
	}

	cases := []struct{ src, want string }{
		{
			`
model Call {
  @Onkt.wsId
  id: string;
}

model Stop {
  reason: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("call") call: Call,
  @Onkt.wsCancel stop: Stop,
}

model Frame {
  payload: FramePayload;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Frame): Frame;
}
`,
			"@ws_cancel variant stop on RPC f must be a message with a @ws_id field",
		},
		{
			`
model Call {
  @Onkt.wsId
  id: string;
}

model A {
  @Onkt.wsId
  id: string;
}

model B {
  @Onkt.wsId
  id: string;
}

@Onkt.discriminator("type")
union FramePayload {
  @Onkt.tag("call") call: Call,
  @Onkt.wsCancel a: A,
  @Onkt.wsCancel b: B,
}

model Frame {
  payload: FramePayload;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Frame): Frame;
}
`,
			"has more than one @ws_cancel variant (a and b)",
		},
	}
	for _, tc := range cases {
		ast, err := parseSrc(`namespace w;
` + tc.src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = Compile([]Source{{Path: "w.tsp", AST: ast}})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want error containing %q, got %v", tc.want, err)
		}
	}
}

// @Onkt.wsCancel takes no argument in TypeSpec; a document that gives
// @ws_cancel one is refused.
func TestWSCancelTakesNoArgument(t *testing.T) {
	q, dec := onktest.Q, onktest.Dec
	call := onktest.Message("Call", onktest.Field("id", "string", dec("ws_id")))
	frame := onktest.Message("Frame", onktest.Oneof("payload", []onkschema.Arg{onktest.Named("discriminator", q("type"))},
		onktest.Variant("call", "Call", dec("ws_cancel", q("x")))))
	file := onktest.File("w", call, frame, onktest.Service("S", "", onktest.RPC("f", "Frame", "Frame", dec("ws", q("/y")))))
	if _, err := Compile([]Source{{Path: "w.tsp", AST: file}}); err == nil || !strings.Contains(err.Error(), "ws_cancel") {
		t.Fatalf("got %v", err)
	}
}

func TestRawFieldValidation(t *testing.T) {
	ok := `namespace w;

model M {
  @Onkt.raw
  a: string;
  @Onkt.raw
  b: bytes;
}
`
	ast, err := parseSrc(ok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile([]Source{{Path: "w.tsp", AST: ast}}); err != nil {
		t.Fatalf("expected @raw string and bytes fields to compile, got %v", err)
	}
	for _, src := range []string{
		`
model M {
  @Onkt.raw
  n: int32;
}
`,
		`
model M {
  @Onkt.raw
  s?: string;
}
`,
		`
model M {
  @Onkt.raw
  s: string[];
}
`,
	} {
		ast, err := parseSrc(`namespace w;
` + src + "\n")
		if err != nil {
			t.Fatal(err)
		}
		_, err = Compile([]Source{{Path: "w.tsp", AST: ast}})
		if err == nil || !strings.Contains(err.Error(), "@raw requires a non-repeated, non-optional string or bytes field") {
			t.Fatalf("%s: want @raw rejection, got %v", src, err)
		}
	}
}

func TestWSTimeoutValidation(t *testing.T) {
	ok := `namespace w;

model Call {
  @Onkt.wsId
  id: string;
  @Onkt.wsTimeout
  @encode(string)
  t: int64;
}

@Onkt.discriminator("type")
union FramePayload {
  call: Call,
}

model Frame {
  payload: FramePayload;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Frame): Frame;
}
`
	ast, err := parseSrc(ok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile([]Source{{Path: "w.tsp", AST: ast}}); err != nil {
		t.Fatalf("expected @ws_timeout next to @ws_id to compile, got %v", err)
	}
	for src, want := range map[string]string{
		`
model M {
  @Onkt.wsTimeout
  t: string;
}
`: "@ws_timeout requires a non-repeated, non-optional integer field",
		`
model Call {
  @Onkt.wsTimeout
  @encode(string)
  t: int64;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Call): Call;
}
`: "must sit next to a @ws_id field",
		`
model Call {
  @Onkt.wsId
  id: string;
  @Onkt.wsTimeout
  @encode(string)
  a: int64;
  @Onkt.wsTimeout
  b: int32;
}

interface S {
  @Onkt.ws
  @route("/y")
  f(...Call): Call;
}
`: "more than one @ws_timeout field",
	} {
		ast, err := parseSrc(`namespace w;
` + src + "\n")
		if err != nil {
			t.Fatal(err)
		}
		_, err = Compile([]Source{{Path: "w.tsp", AST: ast}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want %q, got %v", src, want, err)
		}
	}
}
