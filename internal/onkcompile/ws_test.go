package onkcompile

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onklang"
)

func parseSrc(src string) (*onklang.File, error) { return onklang.Parse(src) }

func TestWSMethodCompiles(t *testing.T) {
	ast, err := parseSrc(`package w

message Msg { room: string
text: string }
message Ev { seq: int64 }

service S {
  base_path: "/v1"
  chat(Msg) -> Ev @ws("/rooms/{room}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "w.onk", AST: ast}})
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
	ast, err := parseSrc(`package w

message RunRequest { code: string }
message RunResult { exit_code: int32 }
message HostCall { id: string @ws_id
method: string }
message HostResult { id: string @ws_id
value: string }

message Frame {
  payload: oneof(discriminator: "type") {
    run: RunRequest @tag("run")
    host_call: HostCall @tag("host_call")
    host_result: HostResult @tag("host_result")
    run_result: RunResult @tag("run_result")
  }
}

service Runtime {
  base_path: "/v1"
  execute(Frame) -> Frame @ws("/execute")
}`)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Compile([]Source{{Path: "w.onk", AST: ast}})
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
			`message M { active: bool @ws_id }
message E { x: int32 }
service S { f(M) -> E @ws("/y") }`,
			"@ws_id requires a non-repeated string or integer field",
		},
		{
			`message M { ids: string[] @ws_id }
message E { x: int32 }
service S { f(M) -> E @ws("/y") }`,
			"@ws_id requires a non-repeated string or integer field",
		},
		{
			`message M { a: string @ws_id
b: string @ws_id }
message E { x: int32 }
service S { f(M) -> E @ws("/y") }`,
			"has more than one @ws_id field",
		},
		{
			`message M { text: string }
message A { id: string @ws_id }
message B { id: int64 @ws_id }
message U {
  payload: oneof(discriminator: "type") {
    a: A @tag("a")
    b: B @tag("b")
  }
}
message E { x: int32 }
service S { f(U) -> E @ws("/y") }`,
			"must share one scalar type",
		},
	}
	for _, tc := range cases {
		src := "package w\n\n" + tc.src
		ast, err := parseSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.want, err)
		}
		if _, err := Compile([]Source{{Path: "w.onk", AST: ast}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q got %v", tc.want, err)
		}
	}
}

func TestWSRejectsConflicts(t *testing.T) {
	cases := []struct{ src, want string }{
		{`service S { f(M) -> E @get("/x") @ws("/y") }`, "@ws cannot be combined with an HTTP verb"},
		{`service S { f(M) -> E @ws("/y") @stream }`, "cannot be combined with @stream"},
		{`service S { f(M) -> E @ws("/y") @body("text") }`, "@ws does not support @body"},
		{`service S { f(M) -> E @ws() }`, "@ws requires one non-empty route"},
	}
	for _, tc := range cases {
		src := "package w\n\nmessage M { text: string }\nmessage E { x: int32 }\n\n" + tc.src
		ast, err := parseSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.want, err)
		}
		if _, err := Compile([]Source{{Path: "w.onk", AST: ast}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q got %v", tc.want, err)
		}
	}
}

func TestWSCancelVariant(t *testing.T) {
	const valid = `package w
message Call { id: string @ws_id }
message Cancel { id: string @ws_id }
message Frame {
  payload: oneof(discriminator: "type") {
    call: Call @tag("call")
    cancel: Cancel @tag("cancel") @ws_cancel
  }
}
service S { f(Frame) -> Frame @ws("/y") }`
	ast, err := parseSrc(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile([]Source{{Path: "w.onk", AST: ast}}); err != nil {
		t.Fatalf("expected a @ws_cancel variant carrying @ws_id to compile, got: %v", err)
	}

	cases := []struct{ src, want string }{
		{
			`message Call { id: string @ws_id }
message Stop { reason: string }
message Frame {
  payload: oneof(discriminator: "type") {
    call: Call @tag("call")
    stop: Stop @ws_cancel
  }
}
service S { f(Frame) -> Frame @ws("/y") }`,
			"@ws_cancel variant stop on RPC f must be a message with a @ws_id field",
		},
		{
			`message Call { id: string @ws_id }
message A { id: string @ws_id }
message B { id: string @ws_id }
message Frame {
  payload: oneof(discriminator: "type") {
    call: Call @tag("call")
    a: A @ws_cancel
    b: B @ws_cancel
  }
}
service S { f(Frame) -> Frame @ws("/y") }`,
			"has more than one @ws_cancel variant (a and b)",
		},
		{
			`message Call { id: string @ws_id }
message Frame {
  payload: oneof(discriminator: "type") {
    call: Call @ws_cancel("x")
  }
}
service S { f(Frame) -> Frame @ws("/y") }`,
			"ws_cancel",
		},
	}
	for _, tc := range cases {
		ast, err := parseSrc("package w\n" + tc.src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = Compile([]Source{{Path: "w.onk", AST: ast}})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want error containing %q, got %v", tc.want, err)
		}
	}
}
