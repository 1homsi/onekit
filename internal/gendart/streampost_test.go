package gendart

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const streamPostSchema = `package app

message TurnRequest { prompt: string }
message Text { text: string }
message TurnEvent {
  payload: oneof(discriminator: "type") {
    text: Text @tag("text")
  }
}

service Agent {
  turn(TurnRequest) -> TurnEvent @post("/turn") @stream
}
`

func TestPostStreamSendsBody(t *testing.T) {
	ast, err := onklang.Parse(streamPostSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	file := pkg.Files[0]
	out := string(GenerateClient(file))
	for _, want := range []string{`request.body = jsonEncode(req.toJson())`, `text/event-stream`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
